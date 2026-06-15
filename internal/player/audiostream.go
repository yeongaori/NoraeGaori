package player

import (
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/pkg/logger"
)

const (
	audioStreamBufSize  = 1500
	tailSampleRate      = 24000.0
	tailSamplesPerFrame = 480
	tailWindowSeconds   = 90
	tailCapacitySamples = 24000 * tailWindowSeconds
	framesPerSecond     = 50.0
	silencePeakLevel    = 0.01
	silenceSampleLevel  = 327
)

func frameSilent(buf []int16) bool {
	for _, s := range buf {
		if s > silenceSampleLevel || s < -silenceSampleLevel {
			return false
		}
	}
	return true
}

func applyGain(buf []int16, factor float64) {
	for i := 0; i < len(buf); i++ {
		sample := float64(buf[i]) * factor
		if sample > 32767 {
			buf[i] = 32767
		} else if sample < -32768 {
			buf[i] = -32768
		} else {
			buf[i] = int16(sample)
		}
	}
}

type streamEndState struct {
	totalFrames      int
	analysis         *TrackAnalysis
	tailStartFrame   int
	silentTailFrames int
}

type audioStream struct {
	pcmChan  chan []int16
	errChan  chan error
	stopChan chan struct{}
	stopOnce sync.Once
	ffmpeg   *exec.Cmd
	endState atomic.Pointer[streamEndState]
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
	out := make([]float32, m.count)
	for i := 0; i < m.count; i++ {
		out[i] = m.buf[(m.start+i)%m.capacity]
	}
	return out, m.produced - int64(m.count)
}

func buildFFmpegArgs(streamURL string, seekSeconds float64, normalization bool) []string {
	args := []string{
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

func startAudioStream(args []string, collectTail bool) (*audioStream, error) {
	ffmpeg := exec.Command("ffmpeg", args...)
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	s := &audioStream{
		pcmChan:  make(chan []int16, audioStreamBufSize),
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
		ffmpeg:   ffmpeg,
	}

	go s.produce(stdout, collectTail)
	return s, nil
}

func (s *audioStream) killFFmpeg() {
	if s.ffmpeg != nil && s.ffmpeg.Process != nil {
		s.ffmpeg.Process.Kill()
	}
}

func (s *audioStream) stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
		s.killFFmpeg()
	})
}

func (s *audioStream) produce(stdout io.Reader, collectTail bool) {
	defer close(s.pcmChan)

	var tail *monoTail
	if collectTail {
		tail = newMonoTail(tailCapacitySamples)
	}

	frameCount := 0
	for {
		pcmBuf := make([]int16, frameSize*channels)

		readErr := make(chan error, 1)
		go func() {
			readErr <- binary.Read(stdout, binary.LittleEndian, &pcmBuf)
		}()

		stallTimer := time.NewTimer(stallTimeout)
		var err error
		select {
		case err = <-readErr:
			stallTimer.Stop()
		case <-stallTimer.C:
			s.killFFmpeg()
			s.errChan <- fmt.Errorf("stream stalled: no data received for %v (after %d frames)", stallTimeout, frameCount)
			return
		case <-s.stopChan:
			stallTimer.Stop()
			s.killFFmpeg()
			s.errChan <- fmt.Errorf("playback stopped by user")
			return
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			waitErr := s.ffmpeg.Wait()
			if waitErr != nil && frameCount == 0 {
				s.errChan <- fmt.Errorf("ffmpeg produced no audio: %w", waitErr)
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

func leadingSilentSamples(samples []float32) int {
	for i, s := range samples {
		if s > silencePeakLevel || s < -silencePeakLevel {
			return i
		}
	}
	return len(samples)
}

func trailingSilentSamples(samples []float32) int {
	last := len(samples) - 1
	for ; last >= 0; last-- {
		if samples[last] > silencePeakLevel || samples[last] < -silencePeakLevel {
			break
		}
	}
	return len(samples) - 1 - last
}

func (s *audioStream) finishEndState(totalFrames int, tail *monoTail) {
	es := &streamEndState{totalFrames: totalFrames}
	if tail != nil {
		samples, startSample := tail.snapshot()
		lead := leadingSilentSamples(samples)
		trail := 0
		if lead < len(samples) {
			trail = trailingSilentSamples(samples)
		}
		es.silentTailFrames = trail / tailSamplesPerFrame
		if es.silentTailFrames > 0 {
			logger.Debugf("[audioStream] trailing silence detected: %d frames (%.1fs)", es.silentTailFrames, float64(es.silentTailFrames)/framesPerSecond)
		}

		audible := samples[lead : len(samples)-trail]
		if analysis, err := analyzeTrackSamples(audible, tailSampleRate); err == nil {
			analysis.FirstBeat += float64(lead) / tailSampleRate
			es.analysis = analysis
		} else {
			logger.Debugf("[audioStream] tail analysis failed: %v", err)
		}
		es.tailStartFrame = int(startSample / tailSamplesPerFrame)
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
