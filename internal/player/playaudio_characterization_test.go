package player

import (
	"strings"
	"testing"
	"time"

	"noraegaori/internal/queue"
)

func boundedAudioStream(frames int) *audioStream {
	s := &audioStream{
		pcmChan:  make(chan []int16, audioStreamBufSize),
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
	}
	go func() {
		defer close(s.pcmChan)
		for i := 0; i < frames; i++ {
			select {
			case <-s.stopChan:
				return
			case s.pcmChan <- make([]int16, frameSize*channels):
			}
		}
	}()
	return s
}

func newCharacterizationPlayer(t *testing.T, guildID string, stream func() *audioStream) (*GuildPlayer, *mockVoiceConn, *queue.Song) {
	t.Helper()

	setupPlayerDB(t, guildID, 1)

	player := GetPlayer(guildID)
	mock := newMockVoiceConn()

	player.mu.Lock()
	player.VoiceConn = mock
	player.StopChan = make(chan struct{})
	player.PendingStream = nil
	player.mu.Unlock()

	previous := newAudioStream
	newAudioStream = func(args []string, collectTail bool) (*audioStream, error) {
		return stream(), nil
	}
	t.Cleanup(func() { newAudioStream = previous })

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
	player, _, song := newCharacterizationPlayer(t, "charend", func() *audioStream { return boundedAudioStream(5) })

	if err := runPlayAudio(t, player, song, make(chan struct{}, 1)); err != nil {
		t.Errorf("got %v, want nil when the stream ends after sending frames", err)
	}
}

func TestPlayAudioReportsAStreamThatSentNothing(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charempty", func() *audioStream { return boundedAudioStream(0) })

	err := runPlayAudio(t, player, song, make(chan struct{}, 1))
	if err == nil {
		t.Fatal("got nil, want an error when the stream produced no frames")
	}
	if !strings.Contains(err.Error(), "no audio frames") {
		t.Errorf("got %v, want an error naming the absent frames", err)
	}
}

func TestPlayAudioRequiresAVoiceConnection(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charnovc", func() *audioStream { return boundedAudioStream(5) })

	player.mu.Lock()
	player.VoiceConn = nil
	player.mu.Unlock()

	err := runPlayAudio(t, player, song, make(chan struct{}, 1))
	if err == nil || !strings.Contains(err.Error(), "voice connection is nil") {
		t.Errorf("got %v, want a nil-voice-connection error", err)
	}
}

func TestPlayAudioBracketsPlaybackWithSpeaking(t *testing.T) {
	player, mock, song := newCharacterizationPlayer(t, "charspeak", func() *audioStream { return boundedAudioStream(5) })

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
	player, _, song := newCharacterizationPlayer(t, "chardone", func() *audioStream { return boundedAudioStream(5) })

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
	player, _, song := newCharacterizationPlayer(t, "charstate", func() *audioStream { return boundedAudioStream(5) })

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
	player, _, song := newCharacterizationPlayer(t, "charfirst", func() *audioStream { return boundedAudioStream(10) })

	firstFrame := make(chan struct{}, 4)
	if err := runPlayAudio(t, player, song, firstFrame); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if len(firstFrame) != 1 {
		t.Errorf("got %d first-frame signals, want exactly 1", len(firstFrame))
	}
}

func TestPlayAudioMarksThePlayerPlaying(t *testing.T) {
	player, _, song := newCharacterizationPlayer(t, "charplaying", func() *audioStream { return boundedAudioStream(30) })

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
