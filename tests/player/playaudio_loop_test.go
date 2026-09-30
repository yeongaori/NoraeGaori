package player_test

import (
	"noraegaori/internal/audio/ffmpeg"
	"testing"
	"time"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func silentAudioStream(frames int) player.HookAudioStream {
	s := newFakeStream(ffmpeg.BufSize)
	s.sendFrames(frames)
	return s
}

func loudAudioStream(frames int) player.HookAudioStream {
	s := newFakeStream(ffmpeg.BufSize)
	go func() {
		defer close(s.pcm)
		for i := 0; i < frames; i++ {
			frame := make([]int16, player.HookFrameSize*player.HookChannels)
			for j := range frame {
				frame[j] = 8000
			}
			select {
			case <-s.stopChan:
				return
			case s.pcm <- frame:
			}
		}
	}()
	return s
}

func runPlayAudioWithFade(t *testing.T, guildPlayer *player.GuildPlayer, song *queue.Song, fade player.HookFadeSettings) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- player.HookPlayAudio(guildPlayer, song, "fake://url", 0, false, 128000, make(chan struct{}, 1), fade, func(*queue.Song) {})
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
	guildPlayer, mock, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return silentAudioStream(20) })

	fade := *player.HookBuildFadeSettings(player.HookFadeSettingsFields{TrimSilence: true})
	if err := runPlayAudioWithFade(t, guildPlayer, song, fade); err != nil {
		t.Fatalf("playAudio returned %v, want nil: skipped silence still counts as progress", err)
	}

	if got := len(mock.opusSend); got != 0 {
		t.Errorf("got %d frames sent, want the leading silence skipped entirely", got)
	}
}

func TestPlayAudioSendsEveryFrameOfALoudStream(t *testing.T) {
	guildID := "looploud"
	guildPlayer, mock, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(12) })

	if err := runPlayAudioWithFade(t, guildPlayer, song, *player.HookBuildFadeSettings(player.HookFadeSettingsFields{TrimSilence: true})); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if got := len(mock.opusSend); got != 12 {
		t.Errorf("got %d frames sent, want all 12 loud frames", got)
	}
}

func TestPlayAudioPublishesFadeInWhileFadingAndClearsItAfter(t *testing.T) {
	guildID := "loopfadein"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(30) })

	fade := *player.HookBuildFadeSettings(player.HookFadeSettingsFields{FadeIn: true, FadeInSec: 10})

	observed := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			guildPlayer.HookMu().Lock()
			fadingIn := guildPlayer.FadingIn
			guildPlayer.HookMu().Unlock()
			if fadingIn {
				observed <- true
				return
			}
			time.Sleep(time.Millisecond)
		}
		observed <- false
	}()

	if err := runPlayAudioWithFade(t, guildPlayer, song, fade); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if !<-observed {
		t.Error("FadingIn was never published while a fade-in was configured")
	}

	guildPlayer.HookMu().Lock()
	fadingIn := guildPlayer.FadingIn
	fadingOut := guildPlayer.FadingOut
	guildPlayer.HookMu().Unlock()

	if fadingIn || fadingOut {
		t.Errorf("got FadingIn=%v FadingOut=%v after playback, want both cleared by the deferred reset", fadingIn, fadingOut)
	}
}

func TestPlayAudioLeavesTransitionArmedClearedOnReturn(t *testing.T) {
	guildID := "looparmed"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(6) })

	guildPlayer.HookTransitionArmed().Store(true)

	if err := runPlayAudioWithFade(t, guildPlayer, song, player.HookFadeSettings{}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if guildPlayer.HookTransitionArmed().Load() {
		t.Error("transitionArmed stayed set after playAudio returned")
	}
}

func TestPlayAudioConsumesTheFadeInNextFlag(t *testing.T) {
	guildID := "loopfadenext"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(6) })

	guildPlayer.HookMu().Lock()
	guildPlayer.FadeInNext = true
	guildPlayer.HookMu().Unlock()

	if err := runPlayAudioWithFade(t, guildPlayer, song, *player.HookBuildFadeSettings(player.HookFadeSettingsFields{FadeIn: true, FadeInSec: 1})); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	guildPlayer.HookMu().Lock()
	fadeInNext := guildPlayer.FadeInNext
	guildPlayer.HookMu().Unlock()

	if fadeInNext {
		t.Error("FadeInNext was not consumed by playAudio")
	}
}
