package player

import (
	"testing"

	"noraegaori/internal/queue"
)

func captureStreamFailures(t *testing.T) *[]string {
	t.Helper()

	reported := []string{}
	previous := reportStreamFailure
	reportStreamFailure = func(url string, err error) {
		reported = append(reported, err.Error())
	}
	t.Cleanup(func() { reportStreamFailure = previous })

	return &reported
}

func TestStreamFetchFailuresAreReportedAgainstTheVersion(t *testing.T) {
	reported := captureStreamFailures(t)
	song := &queue.Song{URL: "https://youtube.com/watch?v=abc"}

	for _, errMsg := range []string{
		"ffmpeg produced no audio: exit status 8: Server returned 403 Forbidden (access denied)",
		"ffmpeg produced no audio: exit status 8",
		"HTTP error 403 Forbidden",
	} {
		reportPlaybackFailure(song, errMsg)
	}

	if len(*reported) != 3 {
		t.Errorf("got %d reported failures, want 3: these are the errors that mean the binary is producing dead URLs", len(*reported))
	}
}

func TestUnrelatedPlaybackErrorsAreNotBlamedOnTheVersion(t *testing.T) {
	reported := captureStreamFailures(t)
	song := &queue.Song{URL: "https://youtube.com/watch?v=abc"}

	for _, errMsg := range []string{
		"playback stopped by user",
		"voice connection died: websocket closed",
		"stream stalled: no data received for 30s (after 120 frames)",
		"voice connection is nil",
		"playback completed with no audio frames sent",
		"opus encoding error",
	} {
		reportPlaybackFailure(song, errMsg)
	}

	if len(*reported) != 0 {
		t.Errorf("got %v reported, want none: blaming the yt-dlp version for these would blacklist healthy binaries", *reported)
	}
}

func TestStreamFetchFailureClassification(t *testing.T) {
	cases := map[string]bool{
		"ffmpeg produced no audio: exit status 8": true,
		"Server returned 403 Forbidden":           true,
		"playback stopped by user":                false,
		"stream stalled: no data received":        false,
		"voice connection died":                   false,
		"playback completed with no audio frames": false,
	}

	for errMsg, want := range cases {
		if got := isStreamFetchFailure(errMsg); got != want {
			t.Errorf("isStreamFetchFailure(%q) = %v, want %v", errMsg, got, want)
		}
	}
}
