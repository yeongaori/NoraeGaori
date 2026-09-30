package ffmpeg_test

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
)

func fakeFFmpeg(t *testing.T, body string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake executables are not portable to windows")
	}
	path := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatalf("failed to write the fake ffmpeg: %v", err)
	}
	return path
}

func frameBytes(frames int) string {
	return strconv.Itoa(frames * dsp.FrameSize * dsp.Channels * 2)
}

type exitCounter struct {
	calls atomic.Int32
}

func (counter *exitCounter) count() {
	counter.calls.Add(1)
}

func drain(t *testing.T, stream *ffmpeg.Stream) int {
	t.Helper()

	frames := 0
	timeout := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-stream.PCM():
			if !ok {
				return frames
			}
			frames++
		case <-timeout:
			t.Fatal("the stream never closed")
		}
	}
}

func TestStartRunsTheGivenBinary(t *testing.T) {
	binary := fakeFFmpeg(t, "head -c "+frameBytes(2)+" /dev/zero")
	counter := &exitCounter{}

	stream, err := ffmpeg.Start(binary, []string{"-i", "input"}, false, counter.count)
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}

	if frames := drain(t, stream); frames != 2 {
		t.Errorf("got %d frames, want the 2 the binary wrote", frames)
	}
	if counter.calls.Load() != 1 {
		t.Errorf("got %d exit calls by the time the PCM closed, want exactly 1", counter.calls.Load())
	}
	if stream.EndState() == nil || stream.EndState().TotalFrames != 2 {
		t.Errorf("got end state %+v, want 2 frames recorded", stream.EndState())
	}
}

func TestStopReportsTheExitOnce(t *testing.T) {
	binary := fakeFFmpeg(t, "exec sleep 30")
	counter := &exitCounter{}

	stream, err := ffmpeg.Start(binary, nil, false, counter.count)
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	stream.Stop()
	stream.Stop()

	drain(t, stream)
	if counter.calls.Load() != 1 {
		t.Errorf("got %d exit calls, want exactly 1 after the killed process was reaped", counter.calls.Load())
	}
}

func TestStopReportsTheExitOnlyAfterTheProcessIsGone(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	binary := fakeFFmpeg(t, "echo $$ > "+pidFile+"; exec sleep 30")
	var stillRunning atomic.Bool
	onExit := func() {
		content, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(string(content)))
		process, err := os.FindProcess(pid)
		if err == nil && process.Signal(syscall.Signal(0)) == nil {
			stillRunning.Store(true)
		}
	}

	stream, err := ffmpeg.Start(binary, nil, false, onExit)
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if content, err := os.ReadFile(pidFile); err == nil && len(content) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake ffmpeg never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stream.Stop()
	drain(t, stream)
	if stillRunning.Load() {
		t.Error("the exit was reported while the killed process still existed, so its build could be removed under it")
	}
}

func TestAFailedRunStillReportsItsExit(t *testing.T) {
	binary := fakeFFmpeg(t, "echo 'no such input' >&2; exit 1")
	counter := &exitCounter{}

	stream, err := ffmpeg.Start(binary, nil, false, counter.count)
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}

	drain(t, stream)
	if counter.calls.Load() != 1 {
		t.Errorf("got %d exit calls, want exactly 1", counter.calls.Load())
	}
	select {
	case err := <-stream.Errs():
		if err == nil || !strings.Contains(err.Error(), "no such input") {
			t.Errorf("got %v, want the failure with ffmpeg's stderr", err)
		}
	default:
		t.Error("the failure was not reported")
	}
}

func TestStartPipeFeedsStdinAndClosesIt(t *testing.T) {
	binary := fakeFFmpeg(t, "cat")
	counter := &exitCounter{}
	reader, writer := io.Pipe()
	go func() {
		writer.Write(make([]byte, 3*dsp.FrameSize*dsp.Channels*2))
		writer.Close()
	}()

	stream, err := ffmpeg.StartPipe(binary, nil, reader, false, counter.count)
	if err != nil {
		t.Fatalf("StartPipe returned %v, want nil", err)
	}

	if frames := drain(t, stream); frames != 3 {
		t.Errorf("got %d frames, want the 3 piped through", frames)
	}
	if counter.calls.Load() != 1 {
		t.Errorf("got %d exit calls, want exactly 1", counter.calls.Load())
	}
}

type closeRecorder struct {
	io.Reader
	closed atomic.Bool
}

func (recorder *closeRecorder) Close() error {
	recorder.closed.Store(true)
	return nil
}

func TestStartPipeClosesStdinWhenTheBinaryIsMissing(t *testing.T) {
	stdin := &closeRecorder{Reader: strings.NewReader("")}
	counter := &exitCounter{}

	if _, err := ffmpeg.StartPipe(filepath.Join(t.TempDir(), "missing"), nil, stdin, false, counter.count); err == nil {
		t.Fatal("StartPipe returned nil, want an error for a missing binary")
	}
	if !stdin.closed.Load() {
		t.Error("the input pipe was left open, so the yt-dlp behind it keeps running")
	}
	if counter.calls.Load() != 0 {
		t.Error("the exit callback ran for a process that never started")
	}
}
