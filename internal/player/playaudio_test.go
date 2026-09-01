package player

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil"
)

func boundedAudioStream(frames int) audioStream {
	s := newFakeStream(ffmpeg.BufSize)
	s.sendFrames(frames)
	return s
}

func newCharacterizationPlayer(t *testing.T, guildID string, stream func() audioStream) (*GuildPlayer, *mockVoiceConn, *queue.Song) {
	t.Helper()

	setupPlayerDB(t, guildID, 1)

	player := GetPlayer(guildID)
	mock := newMockVoiceConn()

	player.mu.Lock()
	player.VoiceConn = mock
	player.StopChan = make(chan struct{})
	player.PendingStream = nil
	player.mu.Unlock()

	testutil.Swap(t, &newAudioStream, func(args []string, collectTail bool) (audioStream, error) {
		return stream(), nil
	})

	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		t.Fatalf("queue not ready: %v", err)
	}

	return player, mock, q.Songs[0]
}

func runPlayAudio(t *testing.T, player *GuildPlayer, song *queue.Song, firstFrameCh chan struct{}) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- playAudio(player, song, "fake://url", 0, false, 128000, firstFrameCh, fadeSettings{}, func(*queue.Song) {})
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("playAudio did not return")
		return nil
	}
}

func TestPlayAudioReturnsNilOnNaturalEnd(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charend", func() audioStream { return boundedAudioStream(5) })

	if err := runPlayAudio(t, player, song, make(chan struct{}, 1)); err != nil {
		t.Errorf("got %v, want nil when the stream ends after sending frames", err)
	}
}

func TestPlayAudioReportsAStreamThatSentNothing(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charempty", func() audioStream { return boundedAudioStream(0) })

	err := runPlayAudio(t, player, song, make(chan struct{}, 1))
	if err == nil {
		t.Fatal("got nil, want an error when the stream produced no frames")
	}
	if !strings.Contains(err.Error(), "no audio frames") {
		t.Errorf("got %v, want an error naming the absent frames", err)
	}
}

func TestPlayAudioRequiresAVoiceConnection(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charnovc", func() audioStream { return boundedAudioStream(5) })

	player.mu.Lock()
	player.VoiceConn = nil
	player.mu.Unlock()

	err := runPlayAudio(t, player, song, make(chan struct{}, 1))
	if err == nil || !strings.Contains(err.Error(), "voice connection is nil") {
		t.Errorf("got %v, want a nil-voice-connection error", err)
	}
}

func TestPlayAudioBracketsPlaybackWithSpeaking(t *testing.T) {
	player, mock, song := newCharacterizationPlayer(t, "charspeak", func() audioStream { return boundedAudioStream(5) })

	if err := runPlayAudio(t, player, song, make(chan struct{}, 1)); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	mock.mu.Lock()
	speaking := append([]bool(nil), mock.speaking...)
	mock.mu.Unlock()

	if len(speaking) < 2 {
		t.Fatalf("got %d Speaking calls, want at least an on and an off", len(speaking))
	}
	if !speaking[0] {
		t.Error("the first Speaking call was false, want playback to open with Speaking(true)")
	}
	if speaking[len(speaking)-1] {
		t.Error("the last Speaking call was true, want playback to close with Speaking(false)")
	}
	if mock.disconnectCount() != 0 {
		t.Errorf("got %d disconnects, want playAudio to leave the voice connection open", mock.disconnectCount())
	}
}

func TestPlayAudioSignalsPlaybackDone(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "chardone", func() audioStream { return boundedAudioStream(5) })

	if err := runPlayAudio(t, player, song, make(chan struct{}, 1)); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	select {
	case <-player.PlaybackDone:
	case <-time.After(2 * time.Second):
		t.Error("playAudio returned without signalling PlaybackDone")
	}
}

func TestPlayAudioClearsTransientStateOnReturn(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charstate", func() audioStream { return boundedAudioStream(5) })

	player.mu.Lock()
	player.FadingIn = true
	player.FadingOut = true
	player.mu.Unlock()
	player.transitionArmed.Store(true)

	if err := runPlayAudio(t, player, song, make(chan struct{}, 1)); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	player.mu.Lock()
	fadingIn, fadingOut := player.FadingIn, player.FadingOut
	player.mu.Unlock()

	if fadingIn {
		t.Error("FadingIn was left set after playAudio returned")
	}
	if fadingOut {
		t.Error("FadingOut was left set after playAudio returned")
	}
	if player.transitionArmed.Load() {
		t.Error("transitionArmed was left set after playAudio returned")
	}
}

func TestPlayAudioSignalsTheFirstFrameOnce(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charfirst", func() audioStream { return boundedAudioStream(10) })

	firstFrame := make(chan struct{}, 4)
	if err := runPlayAudio(t, player, song, firstFrame); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if len(firstFrame) != 1 {
		t.Errorf("got %d first-frame signals, want exactly 1", len(firstFrame))
	}
}

func TestPlayAudioMarksThePlayerPlaying(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charplaying", func() audioStream { return boundedAudioStream(30) })

	observed := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			player.mu.Lock()
			playing, loading := player.Playing, player.Loading
			player.mu.Unlock()
			if playing && !loading {
				observed <- true
				return
			}
			time.Sleep(time.Millisecond)
		}
		observed <- false
	}()

	if err := runPlayAudio(t, player, song, make(chan struct{}, 1)); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if !<-observed {
		t.Error("playAudio never reported the player as playing and not loading")
	}
}

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

func TestAdjustEndStateForOffsetLeavesUnshiftedStreams(t *testing.T) {
	original := &ffmpeg.EndState{TotalFrames: 100, TailStartFrame: 80, SilentTailFrames: 5}

	for _, offset := range []int{0, -1} {
		if got := adjustEndStateForOffset(original, offset); got != original {
			t.Errorf("offset %d returned a copy, want the original untouched", offset)
		}
	}
}

func TestAdjustEndStateForOffsetShiftsFrameCounts(t *testing.T) {
	original := &ffmpeg.EndState{TotalFrames: 100, TailStartFrame: 80, SilentTailFrames: 5}

	got := adjustEndStateForOffset(original, 30)

	if got == original {
		t.Fatal("the original end state was mutated in place")
	}
	if got.TotalFrames != 70 {
		t.Errorf("totalFrames = %d, want 70", got.TotalFrames)
	}
	if got.TailStartFrame != 50 {
		t.Errorf("tailStartFrame = %d, want 50", got.TailStartFrame)
	}
	if got.SilentTailFrames != 5 {
		t.Errorf("silentTailFrames = %d, want it carried through unchanged", got.SilentTailFrames)
	}
	if original.TotalFrames != 100 || original.TailStartFrame != 80 {
		t.Error("the original end state was modified")
	}
}

func TestFadeInGainIsInactiveWithoutAWindow(t *testing.T) {
	for _, frames := range []int{0, -1} {
		gain, active := fadeInGainAt(10, 0, frames)
		if active {
			t.Errorf("frames=%d reported an active fade-in", frames)
		}
		if gain != 1.0 {
			t.Errorf("frames=%d returned gain %g, want unity", frames, gain)
		}
	}
}

func TestFadeInGainRunsFromSilenceToUnity(t *testing.T) {
	const start, frames = 100, 50

	if gain, active := fadeInGainAt(start, start, frames); !active || gain != 0 {
		t.Errorf("at the first frame got (%g, %v), want (0, true)", gain, active)
	}

	mid, active := fadeInGainAt(start+frames/2, start, frames)
	if !active {
		t.Fatal("the midpoint reported an inactive fade-in")
	}
	if mid <= 0 || mid >= 1 {
		t.Errorf("midpoint gain %g, want strictly between 0 and 1", mid)
	}

	if _, active := fadeInGainAt(start+frames, start, frames); active {
		t.Error("the frame past the window still reported an active fade-in")
	}
}

func TestFadeInGainIsMonotonic(t *testing.T) {
	const start, frames = 0, 64

	previous := -1.0
	for frame := start; frame < start+frames; frame++ {
		gain, active := fadeInGainAt(frame, start, frames)
		if !active {
			t.Fatalf("frame %d reported an inactive fade-in inside the window", frame)
		}
		if gain < previous {
			t.Errorf("frame %d gain %g dropped below the previous %g", frame, gain, previous)
		}
		if gain < 0 || gain > 1 {
			t.Errorf("frame %d gain %g is outside [0,1]", frame, gain)
		}
		previous = gain
	}
}

func TestFadeOutGainIsInactiveBeforeItsWindow(t *testing.T) {
	gain, active := fadeOutGainAt(10, 100, 50)
	if active {
		t.Error("a frame before the window reported an active fade-out")
	}
	if gain != 1.0 {
		t.Errorf("got gain %g before the window, want unity", gain)
	}

	if _, active := fadeOutGainAt(200, 100, 0); active {
		t.Error("a zero-length window reported an active fade-out")
	}
}

func TestFadeOutGainRunsFromUnityToSilence(t *testing.T) {
	const start, frames = 100, 50

	if gain, active := fadeOutGainAt(start, start, frames); !active || gain != 1 {
		t.Errorf("at the first frame got (%g, %v), want (1, true)", gain, active)
	}

	end, active := fadeOutGainAt(start+frames, start, frames)
	if !active {
		t.Fatal("the final frame reported an inactive fade-out")
	}
	if end != 0 {
		t.Errorf("final gain %g, want 0", end)
	}
}

func TestFadeOutGainIsMonotonic(t *testing.T) {
	const start, frames = 0, 64

	previous := math.Inf(1)
	for frame := start; frame <= start+frames; frame++ {
		gain, active := fadeOutGainAt(frame, start, frames)
		if !active {
			t.Fatalf("frame %d reported an inactive fade-out inside the window", frame)
		}
		if gain > previous {
			t.Errorf("frame %d gain %g rose above the previous %g", frame, gain, previous)
		}
		if gain < 0 || gain > 1 {
			t.Errorf("frame %d gain %g is outside [0,1]", frame, gain)
		}
		previous = gain
	}
}

func TestFadeGainsClampOutsideTheirWindows(t *testing.T) {
	if gain, active := fadeInGainAt(-10, 0, 50); !active || gain != 0 {
		t.Errorf("a frame before the fade-in start gave (%g, %v), want (0, true)", gain, active)
	}

	if gain, active := fadeOutGainAt(1000, 100, 50); !active || gain != 0 {
		t.Errorf("a frame far past the fade-out end gave (%g, %v), want (0, true)", gain, active)
	}
}

func TestFadeInAndFadeOutAreComplementary(t *testing.T) {
	const frames = 32

	for frame := 0; frame < frames; frame++ {
		in, _ := fadeInGainAt(frame, 0, frames)
		out, _ := fadeOutGainAt(frame, 0, frames)

		if math.Abs((in*in)+(out*out)-1) > 1e-9 {
			t.Errorf("frame %d: fade-in %g and fade-out %g are not equal-power complements", frame, in, out)
		}
	}
}
