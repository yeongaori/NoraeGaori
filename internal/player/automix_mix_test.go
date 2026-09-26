package player

import (
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/audiotest"
	"testing"
)

func mixInPhaseTones(t *testing.T, amplitude float64, frames int) []int16 {
	t.Helper()

	encoder, err := opus.NewEncoder(frameRate, channels)
	if err != nil {
		t.Fatalf("opus encoder: %v", err)
	}
	recipe := transition.DefaultRecipe()
	recipe.Volume = transition.VolumeOverlap

	cs := newCrossfadeState()
	cs.crossfadeFrames = 200
	cs.processor = transition.NewProcessor(recipe, 200, 0.5)
	conn := newMockVoiceConn()

	aTone := &audiotest.ToneGenerator{Frequency: 440, Amplitude: amplitude}
	bTone := &audiotest.ToneGenerator{Frequency: 440, Amplitude: amplitude}
	aFrame := make([]int16, frameSize*channels)
	bFrame := make([]int16, frameSize*channels)

	peaks := make([]int16, 0, frames)
	for i := 0; i < frames; i++ {
		aTone.Fill(aFrame)
		bTone.Fill(bFrame)
		if err := cs.mixAndSend(nil, conn, make(chan struct{}), aFrame, bFrame, 1.0, encoder); err != nil {
			t.Fatalf("mixAndSend: %v", err)
		}
		<-conn.opusSend
		cs.mixedFrames++

		var peak int16
		run, longestRun := 0, 0
		for j := 0; j < len(cs.mixBuf); j += channels {
			sample := cs.mixBuf[j]
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
	peaks := mixInPhaseTones(t, 30000, 60)

	if last := peaks[len(peaks)-1]; last < 32000 {
		t.Errorf("mid-mix peak = %d, want the limiter to hold the level near full scale", last)
	}
}

func TestOverlapOfTwoQuietSongsKeepsItsLevel(t *testing.T) {
	peaks := mixInPhaseTones(t, 6000, 60)

	if last := peaks[len(peaks)-1]; last < 10000 || last > 10300 {
		t.Errorf("mid-mix peak = %d, want the unlimited 0.85+0.85 sum of about 10200", last)
	}
}

func armLoopTransition(t *testing.T, guildID string, periodSec float64) (*GuildPlayer, *crossfadeState) {
	t.Helper()

	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	next := stubAudioStream(t).(*fakeStream)
	next.setEndState(&ffmpeg.EndState{TotalFrames: 1})

	fade := fadeSettings{
		autoMix:      true,
		crossfade:    true,
		autoMixBeats: 16,
		repeatMode:   queue.RepeatOff,
		styleOverrides: transition.StyleOverrides{
			Volume: "fadein_cutout", EQ: "none", Filter: "none", Effect: "none", Loop: "four_beats",
		},
	}
	endState := &ffmpeg.EndState{
		TotalFrames:    20000,
		TailStartFrame: 18000,
		Analysis:       &analysis.TrackAnalysis{BPM: 60 / periodSec, PeriodSec: periodSec},
	}

	player := GetPlayer(guildID)
	cs := newCrossfadeState()
	if !cs.plan(player, endState, 100, fade, false, 128000) {
		t.Fatal("plan returned false, want an armed loop transition")
	}
	defer func() {
		player.mu.Lock()
		player.PendingStream = nil
		player.mu.Unlock()
	}()

	return player, cs
}

func TestLoopStyleArmsABeatLoopOfTheExactLength(t *testing.T) {
	_, cs := armLoopTransition(t, "looparms", 60.0/128)

	if cs.beatLoop == nil {
		t.Fatal("the four_beats style armed no beat loop")
	}
	frame := make([]int16, frameSize*channels)
	for i := 0; i < 93; i++ {
		cs.beatLoop.Next(frame)
	}
	if cs.beatLoop.IsReady() {
		t.Error("ready after 89280 samples, want 90000 plus the seam before the loop can replay")
	}
	cs.beatLoop.Next(frame)
	if !cs.beatLoop.IsReady() {
		t.Error("not ready after 90240 samples, want ready once 90000 plus the 240-sample seam are captured")
	}
}
