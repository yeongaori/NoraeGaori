package player

import (
	"errors"
	"noraegaori/internal/audio/ffmpeg"
	"strings"
	"testing"

	"noraegaori/internal/queue"
)

func drainedStream(t *testing.T, streamErr error) *fakeStream {
	t.Helper()

	s := newFakeStream(0)
	if streamErr != nil {
		s.errs <- streamErr
	}
	return s
}

func TestClassifyDrainedStreamSurfacesAStreamError(t *testing.T) {
	want := errors.New("ffmpeg exploded")
	stream := drainedStream(t, want)

	got := classifyDrainedStream(stream, &queue.Song{}, 100, 0, false)
	if !errors.Is(got, want) {
		t.Errorf("got %v, want the stream error", got)
	}
}

func TestClassifyDrainedStreamAcceptsANormalEnd(t *testing.T) {
	stream := drainedStream(t, nil)

	if err := classifyDrainedStream(stream, &queue.Song{}, 500, 0, false); err != nil {
		t.Errorf("got %v, want nil after frames were sent", err)
	}
}

func TestClassifyDrainedStreamRejectsASilentStream(t *testing.T) {
	stream := drainedStream(t, nil)

	err := classifyDrainedStream(stream, &queue.Song{}, 0, 0, false)
	if err == nil {
		t.Fatal("got nil, want an error when no frames were sent")
	}
	if !strings.Contains(err.Error(), "no audio frames") {
		t.Errorf("got %v, want an error naming the absent frames", err)
	}
}

func TestClassifyDrainedStreamAcceptsAnEmptyHandoff(t *testing.T) {
	stream := drainedStream(t, nil)
	stream.setEndState(&ffmpeg.EndState{})

	if err := classifyDrainedStream(stream, &queue.Song{}, 0, 10, true); err != nil {
		t.Errorf("got %v, want nil when a handoff resumed past the end", err)
	}
}

func TestClassifyDrainedStreamStillRejectsAHandoffWithoutEndState(t *testing.T) {
	stream := drainedStream(t, nil)

	if err := classifyDrainedStream(stream, &queue.Song{}, 0, 10, true); err == nil {
		t.Error("got nil, want an error when the handoff carried no end state")
	}
}

func TestClassifyDrainedStreamPrefersTheStreamErrorOverFrameCount(t *testing.T) {
	want := errors.New("ffmpeg died early")
	stream := drainedStream(t, want)

	got := classifyDrainedStream(stream, &queue.Song{}, 0, 0, false)
	if !errors.Is(got, want) {
		t.Errorf("got %v, want the stream error to win over the frame-count check", got)
	}
}
