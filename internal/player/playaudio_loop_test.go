package player

import (
	"testing"
	"time"

	"noraegaori/internal/queue"
)

func silentAudioStream(frames int) *audioStream {
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

func loudAudioStream(frames int) *audioStream {
	s := &audioStream{
		pcmChan:  make(chan []int16, audioStreamBufSize),
		errChan:  make(chan error, 1),
		stopChan: make(chan struct{}),
	}
	go func() {
		defer close(s.pcmChan)
		for i := 0; i < frames; i++ {
			frame := make([]int16, frameSize*channels)
			for j := range frame {
				frame[j] = 8000
			}
			select {
			case <-s.stopChan:
				return
			case s.pcmChan <- frame:
			}
		}
	}()
	return s
}

func runPlayAudioWithFade(t *testing.T, player *GuildPlayer, song *queue.Song, fade fadeSettings) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- playAudio(player, song, "fake://url", 0, false, 128000, make(chan struct{}, 1), fade, func(*queue.Song) {})
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("playAudio did not return")
		return nil
	}
}

func TestPlayAudioSkipsLeadingSilenceWithoutSendingIt(t *testing.T) {
	guildID := "loopskip"
	player, mock, song := newCharacterizationPlayer(t, guildID, func() *audioStream { return silentAudioStream(20) })

	fade := fadeSettings{trimSilence: true}
	if err := runPlayAudioWithFade(t, player, song, fade); err != nil {
		t.Fatalf("playAudio returned %v, want nil: skipped silence still counts as progress", err)
	}

	if got := len(mock.opusSend); got != 0 {
		t.Errorf("got %d frames sent, want the leading silence skipped entirely", got)
	}
}

func TestPlayAudioSendsEveryFrameOfALoudStream(t *testing.T) {
	guildID := "looploud"
	player, mock, song := newCharacterizationPlayer(t, guildID, func() *audioStream { return loudAudioStream(12) })

	if err := runPlayAudioWithFade(t, player, song, fadeSettings{trimSilence: true}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if got := len(mock.opusSend); got != 12 {
		t.Errorf("got %d frames sent, want all 12 loud frames", got)
	}
}

func TestPlayAudioPublishesFadeInWhileFadingAndClearsItAfter(t *testing.T) {
	guildID := "loopfadein"
	player, _, song := newCharacterizationPlayer(t, guildID, func() *audioStream { return loudAudioStream(30) })

	fade := fadeSettings{fadeIn: true, fadeInSec: 10}

	observed := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			player.mu.Lock()
			fadingIn := player.FadingIn
			player.mu.Unlock()
			if fadingIn {
				observed <- true
				return
			}
			time.Sleep(time.Millisecond)
		}
		observed <- false
	}()

	if err := runPlayAudioWithFade(t, player, song, fade); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if !<-observed {
		t.Error("FadingIn was never published while a fade-in was configured")
	}

	player.mu.Lock()
	fadingIn := player.FadingIn
	fadingOut := player.FadingOut
	player.mu.Unlock()

	if fadingIn || fadingOut {
		t.Errorf("got FadingIn=%v FadingOut=%v after playback, want both cleared by the deferred reset", fadingIn, fadingOut)
	}
}

func TestPlayAudioLeavesTransitionArmedClearedOnReturn(t *testing.T) {
	guildID := "looparmed"
	player, _, song := newCharacterizationPlayer(t, guildID, func() *audioStream { return loudAudioStream(6) })

	player.transitionArmed.Store(true)

	if err := runPlayAudioWithFade(t, player, song, fadeSettings{}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if player.transitionArmed.Load() {
		t.Error("transitionArmed stayed set after playAudio returned")
	}
}

func TestPlayAudioConsumesTheFadeInNextFlag(t *testing.T) {
	guildID := "loopfadenext"
	player, _, song := newCharacterizationPlayer(t, guildID, func() *audioStream { return loudAudioStream(6) })

	player.mu.Lock()
	player.FadeInNext = true
	player.mu.Unlock()

	if err := runPlayAudioWithFade(t, player, song, fadeSettings{fadeIn: true, fadeInSec: 1}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	player.mu.Lock()
	fadeInNext := player.FadeInNext
	player.mu.Unlock()

	if fadeInNext {
		t.Error("FadeInNext was not consumed by playAudio")
	}
}
