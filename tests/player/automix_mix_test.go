package player_test

import (
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/audiotest"
	"testing"
)

func mixInPhaseTones(t *testing.T, amplitude, volume float64, frames int) []int16 {
	t.Helper()

	encoder, err := opus.NewEncoder(player.HookFrameRate, player.HookChannels)
	if err != nil {
		t.Fatalf("opus encoder: %v", err)
	}
	recipe := transition.DefaultRecipe()
	recipe.Volume = transition.VolumeOverlap

	cs := player.HookNewCrossfadeState()
	*cs.HookCrossfadeFrames() = 200
	*cs.HookProcessor() = transition.NewProcessor(recipe, 200, 0.5)
	conn := newMockVoiceConn()

	aTone := &audiotest.ToneGenerator{Frequency: 440, Amplitude: amplitude}
	bTone := &audiotest.ToneGenerator{Frequency: 440, Amplitude: amplitude}
	aFrame := make([]int16, player.HookFrameSize*player.HookChannels)
	bFrame := make([]int16, player.HookFrameSize*player.HookChannels)

	peaks := make([]int16, 0, frames)
	for i := 0; i < frames; i++ {
		aTone.Fill(aFrame)
		bTone.Fill(bFrame)
		if err := cs.HookMixAndSend(nil, conn, make(chan struct{}), aFrame, bFrame, volume, encoder); err != nil {
			t.Fatalf("mixAndSend: %v", err)
		}
		<-conn.opusSend
		*cs.HookMixedFrames()++

		var peak int16
		run, longestRun := 0, 0
		for j := 0; j < len(*cs.HookMixBuf()); j += player.HookChannels {
			sample := (*cs.HookMixBuf())[j]
			if sample == 32767 || sample <= -32767 {
				run++
				longestRun = max(longestRun, run)
			} else {
				run = 0
			}
			if sample > peak {
				peak = sample
			}
		}
		if longestRun > 2 {
			t.Fatalf("frame %d held the rail for %d samples in a row, want a limited peak instead of a clipped plateau", i, longestRun)
		}
		peaks = append(peaks, peak)
	}
	return peaks
}

func TestLoudOverlapIsLimitedJustBelowFullScale(t *testing.T) {
	peaks := mixInPhaseTones(t, 30000, 1.0, 60)

	if last := peaks[len(peaks)-1]; last < 32000 {
		t.Errorf("mid-mix peak = %d, want the limiter to hold the level near full scale", last)
	}
}

func TestOverlapOfTwoQuietSongsKeepsItsLevel(t *testing.T) {
	peaks := mixInPhaseTones(t, 6000, 1.0, 60)

	if last := peaks[len(peaks)-1]; last < 10000 || last > 10300 {
		t.Errorf("mid-mix peak = %d, want the unlimited 0.85+0.85 sum of about 10200", last)
	}
}

func TestHalfVolumeMixIsNotLimited(t *testing.T) {
	peaks := mixInPhaseTones(t, 24000, 0.5, 60)

	if last := peaks[len(peaks)-1]; last < 20200 || last > 20600 {
		t.Errorf("mid-mix peak at 50%% volume = %d, want the unlimited 0.85*2*24000*0.5 = 20400", last)
	}
}

func armLoopTransition(t *testing.T, guildID string, periodSec float64) (*player.GuildPlayer, *player.HookCrossfadeState) {
	t.Helper()

	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	next := stubAudioStream(t).(*fakeStream)
	next.setEndState(&ffmpeg.EndState{TotalFrames: 1})

	fade := *player.HookBuildFadeSettings(player.HookFadeSettingsFields{
		AutoMix:      true,
		Crossfade:    true,
		AutoMixBeats: 16,
		RepeatMode:   queue.RepeatOff,
		StyleOverrides: transition.StyleOverrides{
			Volume: "fadein_cutout", EQ: "none", Filter: "none", Effect: "none", Loop: "four_beats",
		},
	})
	endState := &ffmpeg.EndState{
		TotalFrames:    20000,
		TailStartFrame: 18000,
		Analysis:       &analysis.TrackAnalysis{BPM: 60 / periodSec, PeriodSec: periodSec},
	}

	guildPlayer := player.GetPlayer(guildID)
	cs := player.HookNewCrossfadeState()
	if !cs.HookPlan(guildPlayer, endState, 100, fade, false, 128000) {
		t.Fatal("plan returned false, want an armed loop transition")
	}
	defer func() {
		guildPlayer.HookMu().Lock()
		guildPlayer.PendingStream = nil
		guildPlayer.HookMu().Unlock()
	}()

	return guildPlayer, cs
}

func TestLoopStyleArmsABeatLoopOfTheExactLength(t *testing.T) {
	_, cs := armLoopTransition(t, "looparms", 60.0/128)

	if *cs.HookBeatLoop() == nil {
		t.Fatal("the four_beats style armed no beat loop")
	}
	frame := make([]int16, player.HookFrameSize*player.HookChannels)
	for i := 0; i < 93; i++ {
		(*cs.HookBeatLoop()).Next(frame)
	}
	if (*cs.HookBeatLoop()).IsReady() {
		t.Error("ready after 89280 samples, want 90000 plus the seam before the loop can replay")
	}
	(*cs.HookBeatLoop()).Next(frame)
	if !(*cs.HookBeatLoop()).IsReady() {
		t.Error("not ready after 90240 samples, want ready once 90000 plus the 240-sample seam are captured")
	}
}
