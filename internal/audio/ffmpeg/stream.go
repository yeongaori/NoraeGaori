package ffmpeg

import (
	"encoding/binary"
	"fmt"
	"io"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/internal/logger"
)

const (
	StallTimeout = 30 * time.Second

	BufSize             = 1500
	tailSamplesPerFrame = 480
	tailWindowSeconds   = 90
	tailCapacitySamples = 24000 * tailWindowSeconds
	framesPerSecond     = dsp.FramesPerSecond
	stderrTailBytes     = 2048
)

type EndState struct {
	TotalFrames      int
	Analysis         *analysis.TrackAnalysis
	TailStartFrame   int
	SilentTailFrames int
}

type Stream struct {
	pcmChan  chan []int16
	errChan  chan error
	stopChan chan struct{}
	stopOnce sync.Once
	ffmpeg   *exec.Cmd
	stdin    io.Closer
	endState atomic.Pointer[EndState]
	diag     *stderrTail
}

type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailBytes {
		t.buf = t.buf[len(t.buf)-stderrTailBytes:]
	}
	return len(p), nil
}

func (t *stderrTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return strings.TrimSpace(string(t.buf))
}

type monoTail struct {
	buf      []float32
	capacity int
	count    int
	start    int
	produced int64
}

func newMonoTail(capacity int) *monoTail {
	return &monoTail{buf: make([]float32, capacity), capacity: capacity}
}

func (m *monoTail) append(s float32) {
	if m.count < m.capacity {
		m.buf[(m.start+m.count)%m.capacity] = s
		m.count++
	} else {
		m.buf[m.start] = s
		m.start = (m.start + 1) % m.capacity
	}
	m.produced++
}

func (m *monoTail) snapshot() ([]float32, int64) {
	if m.start != 0 {
		slices.Reverse(m.buf[:m.start])
		slices.Reverse(m.buf[m.start:])
		slices.Reverse(m.buf)
		m.start = 0
	}
	return m.buf[:m.count], m.produced - int64(m.count)
}

func Args(streamURL string, seekSeconds float64, normalization bool) []string {
	args := []string{
		"-hide_banner",
		"-nostats",
		"-loglevel", "error",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
	}

	if seekSeconds > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", seekSeconds))
	}

	args = append(args, "-i", streamURL)

	if normalization {
		args = append(args, "-af", "dynaudnorm=framelen=500:gausssize=31:peak=0.95")
	}

	args = append(args,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"pipe:1",
	)
	return args
}

func PipeArgs(normalization bool) []string {
	args := []string{"-hide_banner", "-nostats", "-loglevel", "error", "-i", "pipe:0"}

	if normalization {
		args = append(args, "-af", "dynaudnorm=framelen=500:gausssize=31:peak=0.95")
	}

	args = append(args,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"pipe:1",
	)
	return args
}

func StartPipe(args []string, stdin io.ReadCloser, collectTail bool) (*Stream, error) {
	ffmpeg := exec.Command("ffmpeg", args...)
	ffmpeg.Stdin = stdin

	diag := &stderrTail{}
	ffmpeg.Stderr = diag

	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	s := &Stream{
		pcmChan:  make(chan []int16, BufSize),
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
		ffmpeg:   ffmpeg,
		stdin:    stdin,
		diag:     diag,
	}

	go s.produce(stdout, collectTail)
	return s, nil
}

func Start(args []string, collectTail bool) (*Stream, error) {
	ffmpeg := exec.Command("ffmpeg", args...)

	diag := &stderrTail{}
	ffmpeg.Stderr = diag

	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	s := &Stream{
		pcmChan:  make(chan []int16, BufSize),
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
		ffmpeg:   ffmpeg,
		diag:     diag,
	}

	go s.produce(stdout, collectTail)
	return s, nil
}

func (s *Stream) PCM() <-chan []int16 {
	return s.pcmChan
}

func (s *Stream) Errs() <-chan error {
	return s.errChan
}

func (s *Stream) EndState() *EndState {
	return s.endState.Load()
}

func (s *Stream) Buffered() int {
	return len(s.pcmChan)
}

func (s *Stream) Diagnostics() string {
	if s.diag == nil {
		return ""
	}

	if detail := s.diag.String(); detail != "" {
		return ": " + detail
	}
	return ""
}

func (s *Stream) killFFmpeg() {
	if s.ffmpeg != nil && s.ffmpeg.Process != nil {
		if err := s.ffmpeg.Process.Kill(); err != nil {
			logger.Debugf("Failed to kill ffmpeg: %v", err)
		}
	}
	if s.stdin != nil {
		s.stdin.Close()
	}
}

func (s *Stream) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
		s.killFFmpeg()
	})
}

func (s *Stream) produce(stdout io.Reader, collectTail bool) {
	defer close(s.pcmChan)
	defer func() {
		if s.stdin != nil {
			s.stdin.Close()
		}
	}()

	var tail *monoTail
	if collectTail {
		tail = newMonoTail(tailCapacitySamples)
	}

	readBuf := make([]byte, dsp.FrameSize*dsp.Channels*2)
	readErr := make(chan error, 1)
	stallTimer := time.NewTimer(StallTimeout)
	defer stallTimer.Stop()

	frameCount := 0
	for {
		pcmBuf := make([]int16, dsp.FrameSize*dsp.Channels)

		go func() {
			_, err := io.ReadFull(stdout, readBuf)
			readErr <- err
		}()

		if !stallTimer.Stop() {
			select {
			case <-stallTimer.C:
			default:
			}
		}
		stallTimer.Reset(StallTimeout)

		var err error
		select {
		case err = <-readErr:
		case <-stallTimer.C:
			s.killFFmpeg()
			s.errChan <- fmt.Errorf("stream stalled: no data received for %v (after %d frames)", StallTimeout, frameCount)
			return
		case <-s.stopChan:
			s.killFFmpeg()
			s.errChan <- fmt.Errorf("playback stopped by user")
			return
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			waitErr := s.ffmpeg.Wait()
			if waitErr != nil && frameCount == 0 {
				s.errChan <- fmt.Errorf("ffmpeg produced no audio: %w%s", waitErr, s.Diagnostics())
				return
			}
			s.finishEndState(frameCount, tail)
			return
		}
		if err != nil {
			s.killFFmpeg()
			s.errChan <- fmt.Errorf("pcm read error: %w", err)
			return
		}

		for i := range pcmBuf {
			pcmBuf[i] = int16(binary.LittleEndian.Uint16(readBuf[i*2:]))
		}

		if tail != nil {
			appendTail(tail, pcmBuf)
		}

		select {
		case s.pcmChan <- pcmBuf:
		case <-s.stopChan:
			s.killFFmpeg()
			s.errChan <- fmt.Errorf("playback stopped by user")
			return
		}

		frameCount++
	}
}

func (s *Stream) finishEndState(totalFrames int, tail *monoTail) {
	es := &EndState{TotalFrames: totalFrames}
	if tail != nil {
		samples, startSample := tail.snapshot()
		lead := dsp.LeadingSilentSamples(samples)
		trail := 0
		if lead < len(samples) {
			trail = dsp.TrailingSilentSamples(samples)
		}
		es.SilentTailFrames = trail / tailSamplesPerFrame
		if es.SilentTailFrames > 0 {
			logger.Debugf("trailing silence detected: %d frames (%.1fs)", es.SilentTailFrames, float64(es.SilentTailFrames)/framesPerSecond)
		}

		audible := samples[lead : len(samples)-trail]
		if tail, err := analysis.AnalyzeTrackSamples(audible, analysis.SampleRate); err == nil {
			tail.FirstBeat += float64(lead) / analysis.SampleRate
			es.Analysis = tail
		} else {
			logger.Debugf("tail analysis failed: %v", err)
		}
		es.TailStartFrame = int(startSample / tailSamplesPerFrame)
	}
	s.endState.Store(es)
}

func appendTail(tail *monoTail, pcmBuf []int16) {
	for i := 0; i+1 < len(pcmBuf); i += 4 {
		l := float64(pcmBuf[i])
		r := float64(pcmBuf[i+1])
		mono := float32(((l + r) / 2) / 32768.0)
		tail.append(mono)
	}
}
