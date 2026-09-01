package ffmpeg

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStderrTailKeepsOnlyTheTail(t *testing.T) {
	tail := &stderrTail{}

	if _, err := tail.Write([]byte(strings.Repeat("a", stderrTailBytes))); err != nil {
		t.Fatalf("Write returned %v, want nil", err)
	}
	if _, err := tail.Write([]byte("THE-LAST-LINE")); err != nil {
		t.Fatalf("Write returned %v, want nil", err)
	}

	got := tail.String()
	if len(got) > stderrTailBytes {
		t.Errorf("kept %d bytes, want at most %d", len(got), stderrTailBytes)
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

	stream, err := Start(Args("/nonexistent/definitely-not-a-media-file", 0, false), false)
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	defer stream.Stop()

	select {
	case produceErr := <-stream.errChan:
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
