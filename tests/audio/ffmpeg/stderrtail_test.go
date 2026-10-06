package ffmpeg_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/audio/ffmpeg"
)

func TestStderrTailKeepsOnlyTheTail(t *testing.T) {
	tail := &ffmpeg.HookStderrTail{}

	if _, err := tail.Write([]byte(strings.Repeat("a", ffmpeg.HookStderrTailBytes))); err != nil {
		t.Fatalf("Write returned %v, want nil", err)
	}
	if _, err := tail.Write([]byte("THE-LAST-LINE")); err != nil {
		t.Fatalf("Write returned %v, want nil", err)
	}

	got := tail.String()
	if len(got) > ffmpeg.HookStderrTailBytes {
		t.Errorf("kept %d bytes, want at most %d", len(got), ffmpeg.HookStderrTailBytes)
	}
	if !strings.HasSuffix(got, "THE-LAST-LINE") {
		t.Error("the most recent output was dropped, which is the part that names the failure")
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed; skipping the test that drives it")
	}
}

func TestFailedFFmpegReportsItsOwnDiagnostics(t *testing.T) {
	requireFFmpeg(t)

	stream, err := ffmpeg.Start("ffmpeg", ffmpeg.Args("/nonexistent/definitely-not-a-media-file", 0, false, ffmpeg.Tempo{}), false, nil)
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	defer stream.Stop()

	select {
	case produceErr := <-*stream.HookErrChan():
		if produceErr == nil {
			t.Fatal("got nil, want a failure for a missing input file")
		}
		if !strings.Contains(produceErr.Error(), "ffmpeg produced no audio") {
			t.Fatalf("got %v, want the no-audio classification", produceErr)
		}
		if !strings.Contains(produceErr.Error(), "definitely-not-a-media-file") {
			t.Errorf("error %q does not carry ffmpeg's own explanation, so the cause stays invisible in the log", produceErr)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ffmpeg never reported a failure")
	}
}
