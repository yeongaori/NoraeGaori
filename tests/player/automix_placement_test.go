package player_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/logtest"
)

func steadyBeat(bpm float64) *analysis.TrackAnalysis {
	return &analysis.TrackAnalysis{
		BPM:           bpm,
		PeriodSec:     60 / bpm,
		FirstBeat:     0.3,
		Duration:      75,
		Tonic:         0,
		KeyConfidence: 0.5,
		BeatStrength:  0.6,
		BarOffsets:    make([]float64, 64),
	}
}

func captureStreamArgs(t *testing.T) *[]string {
	t.Helper()

	var captured []string
	stream := newFakeStream(0)
	testutil.Swap(t, player.HookNewAudioStream, func(_ string, args []string, _ bool, _ func()) (player.HookAudioStream, error) {
		captured = args
		return stream, nil
	})
	return &captured
}

func planAutoMix(t *testing.T, guildID string, incoming *analysis.TrackAnalysis) (*player.HookCrossfadeState, []string) {
	t.Helper()
	return planAutoMixWith(t, guildID, incoming, nil)
}

func planAutoMixWith(t *testing.T, guildID string, incoming *analysis.TrackAnalysis, songOverrides map[string]string) (*player.HookCrossfadeState, []string) {
	t.Helper()

	q := seedCrossfadeQueue(t, guildID)
	if songOverrides != nil {
		if err := queue.SetSongAutoMixOverrides(guildID, q.Songs[0].ID, songOverrides); err != nil {
			t.Fatalf("failed to set the song overrides: %v", err)
		}
	}
	cacheKey := fmt.Sprintf("%s_%d", guildID, q.Songs[1].ID)
	player.HookPreCacheStoreMu.Lock()
	(*player.HookPreCacheStore)[cacheKey] = &player.PreCache{StreamURL: "https://example.invalid/next", SongID: q.Songs[1].ID, Timestamp: time.Now(), Analysis: incoming}
	player.HookPreCacheStoreMu.Unlock()
	t.Cleanup(func() {
		player.HookPreCacheStoreMu.Lock()
		delete(*player.HookPreCacheStore, cacheKey)
		player.HookPreCacheStoreMu.Unlock()
	})
	args := captureStreamArgs(t)

	tail := steadyBeat(128)
	tail.Offset = 90
	tail.Duration = 90
	tail.FirstBeat = 0.2
	endState := &ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 4500, Analysis: tail}
	fade := player.HookBuildFadeSettings(&player.HookFadeSettingsFields{
		AutoMix: true, Crossfade: true, TrimSilence: true, AutoMixBeats: 64, RepeatMode: queue.RepeatOff,
	})

	cs := player.HookNewCrossfadeState()
	if !cs.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000) {
		t.Fatal("plan returned false, want an armed transition")
	}
	t.Cleanup(cs.HookAbort)
	return cs, *args
}

func argumentAfter(args []string, flag string) string {
	if index := slices.Index(args, flag); index >= 0 && index+1 < len(args) {
		return args[index+1]
	}
	return ""
}

func TestBeatmatchedPlanStartsOnTheLastFullBarWindow(t *testing.T) {
	cs, args := planAutoMix(t, "placebeat", steadyBeat(125))

	if *cs.HookTransitionFrame() != 8166 {
		t.Errorf("transition frame %d, want 8166 for the 8-bar window on the downbeat at 163.325s", *cs.HookTransitionFrame())
	}
	if *cs.HookCrossfadeFrames() != 750 {
		t.Errorf("crossfade frames %d, want 750 for 8 bars at 128 BPM", *cs.HookCrossfadeFrames())
	}
	if end := *cs.HookTransitionFrame() + *cs.HookCrossfadeFrames(); end < 9000-94 {
		t.Errorf("transition ends at frame %d, want it inside the last bar before the end at 9000", end)
	}
	if seek := argumentAfter(args, "-ss"); seek != "0.295" {
		t.Errorf("incoming seek %q, want 0.295: the first downbeat at 0.3s pulled back by the 5ms the outgoing downbeat sits into its frame", seek)
	}
	if filter := argumentAfter(args, "-af"); !strings.Contains(filter, "atempo=1.0240") {
		t.Errorf("filter %q, want the incoming song sped to 128/125", filter)
	}
	if *cs.HookTrimBLead() {
		t.Error("a beatmatched plan trims the incoming lead silence, which would push its downbeats off the grid")
	}
}

func TestSessionOriginCountsTheSeekAndTheHandedOffFrames(t *testing.T) {
	cases := []struct {
		baseOffsetMs, frameOffset int
		want                      float64
	}{
		{0, 0, 0},
		{12500, 0, 12.5},
		{4300, 750, 19.3},
	}
	for i := range cases {
		c := &cases[i]
		if got := player.HookSessionOriginSec(c.baseOffsetMs, c.frameOffset); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("origin for seek %dms and %d handed-off frames = %.3fs, want %.3fs", c.baseOffsetMs, c.frameOffset, got, c.want)
		}
	}
}

func TestUnmatchablePlanFadesOverTheLastFiveSeconds(t *testing.T) {
	weak := steadyBeat(125)
	weak.BeatStrength = 0.1
	cs, args := planAutoMix(t, "placefade", weak)

	if *cs.HookTransitionFrame() != 8750 || *cs.HookCrossfadeFrames() != 250 {
		t.Errorf("transition at frame %d for %d frames, want 8750 for 250 (the last 5s)", *cs.HookTransitionFrame(), *cs.HookCrossfadeFrames())
	}
	if seek := argumentAfter(args, "-ss"); seek != "" {
		t.Errorf("incoming seek %q, want the song from its start", seek)
	}
	if filter := argumentAfter(args, "-af"); filter != "" {
		t.Errorf("filter %q, want no tempo change", filter)
	}
	if !*cs.HookTrimBLead() {
		t.Error("a fade from the top of the song keeps its lead silence, want it trimmed")
	}
}

func TestSongBeatmatchOffForcesTheFade(t *testing.T) {
	cs, args := planAutoMixWith(t, "placeforcedfade", steadyBeat(125), map[string]string{"beatmatch": "off"})

	if *cs.HookTransitionFrame() != 8750 || *cs.HookCrossfadeFrames() != 250 {
		t.Errorf("transition at frame %d for %d frames, want the 5s fade a matchable pair gets with beatmatch off", *cs.HookTransitionFrame(), *cs.HookCrossfadeFrames())
	}
	if filter := argumentAfter(args, "-af"); filter != "" {
		t.Errorf("filter %q, want no tempo change", filter)
	}
}

func TestSongLengthPicksTheBarCount(t *testing.T) {
	cs, _ := planAutoMixWith(t, "placeforcedlength", steadyBeat(125), map[string]string{"length": "four_bars"})

	if *cs.HookCrossfadeFrames() != 375 {
		t.Errorf("crossfade frames %d, want 375 for the chosen 4 bars at 128 BPM", *cs.HookCrossfadeFrames())
	}
}

func TestIncomingRollIsArmedOnTheIncomingSong(t *testing.T) {
	cs, _ := planAutoMixWith(t, "placeroll", steadyBeat(125), map[string]string{"fx_in": "roll"})
	if *cs.HookIncomingLoop() == nil {
		t.Fatal("an incoming roll armed no loop on the incoming song")
	}
	if *cs.HookBeatLoop() != nil {
		t.Error("an incoming roll armed a loop on the outgoing song")
	}

	plain, _ := planAutoMix(t, "placenoroll", steadyBeat(125))
	if *plain.HookIncomingLoop() != nil {
		t.Error("a plan without an incoming roll armed one")
	}
}

func TestPlanNamesTheSideWithoutAnalysis(t *testing.T) {
	readLog := logtest.Capture(t)
	planAutoMix(t, "placemissing", nil)

	want := "analysis unavailable (outgoing tail present, next head missing)"
	if !strings.Contains(readLog(), want) {
		t.Errorf("got plan inputs without %q", want)
	}
}

func TestPlanLogsAMissingStreamURLOnce(t *testing.T) {
	const guildID = "placeurlwait"
	readLog := logtest.Capture(t)
	q := seedCrossfadeQueue(t, guildID)
	endState := &ffmpeg.EndState{TotalFrames: 9000}
	fade := player.HookBuildFadeSettings(&player.HookFadeSettingsFields{AutoMix: true, Crossfade: true, AutoMixBeats: 64})

	cs := player.HookNewCrossfadeState()
	t.Cleanup(cs.HookAbort)
	for attempt := range 3 {
		if cs.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000) {
			t.Fatalf("got plan %d armed, want no plan without a cached stream URL", attempt)
		}
	}

	line := fmt.Sprintf("next song %s has no cached stream URL yet for guild: %s", q.Songs[1].Title, guildID)
	if got := countLogLines(readLog(), line); got != 1 {
		t.Errorf("got %d missing stream URL lines over 3 plans, want 1", got)
	}

	next := player.HookNewCrossfadeState()
	t.Cleanup(next.HookAbort)
	next.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000)
	if got := countLogLines(readLog(), line); got != 2 {
		t.Errorf("got %d missing stream URL lines after a new session, want 2", got)
	}
}

func TestPlanWaitsForAPendingHeadAnalysis(t *testing.T) {
	setup := func(t *testing.T, guildID string, pendingIndex, beats int) (*player.HookCrossfadeState, *ffmpeg.EndState, *player.HookFadeSettings, *queue.Song) {
		t.Helper()

		q := seedCrossfadeQueue(t, guildID)
		next := q.Songs[1]
		pending := q.Songs[pendingIndex]
		cacheNextStreamURL(t, guildID, next.ID, "https://example.invalid/next")
		player.HookMarkAnalysisPending(guildID, pending.ID)
		t.Cleanup(func() { player.HookClearAnalysisPending(guildID, pending.ID) })
		captureStreamArgs(t)

		tail := steadyBeat(128)
		tail.Offset = 90
		tail.Duration = 90
		tail.FirstBeat = 0.2
		endState := &ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 4500, Analysis: tail}
		fade := player.HookBuildFadeSettings(&player.HookFadeSettingsFields{
			AutoMix: true, Crossfade: true, TrimSilence: true, AutoMixBeats: beats, RepeatMode: queue.RepeatOff,
		})
		cs := player.HookNewCrossfadeState()
		t.Cleanup(cs.HookAbort)
		return cs, endState, fade, next
	}

	t.Run("plenty of time", func(t *testing.T) {
		const guildID = "placewait"
		cs, endState, fade, _ := setup(t, guildID, 1, 64)

		if cs.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000) {
			t.Errorf("got a plan with %.1fs left and the head analysis pending, want a wait", float64(9000-100)/50)
		}
	})

	t.Run("at the deadline", func(t *testing.T) {
		const guildID = "placewaitdeadline"
		readLog := logtest.Capture(t)
		cs, endState, fade, _ := setup(t, guildID, 1, 64)

		if !cs.HookPlan(player.GetPlayer(guildID), endState, 7400, 0, fade, false, 128000) {
			t.Fatal("got no plan at the deadline, want one without the head analysis")
		}
		if want := "analysis unavailable (outgoing tail present, next head missing)"; !strings.Contains(readLog(), want) {
			t.Errorf("got plan inputs without %q", want)
		}
	})

	t.Run("analysis arrives", func(t *testing.T) {
		const guildID = "placewaitarrives"
		readLog := logtest.Capture(t)
		cs, endState, fade, next := setup(t, guildID, 1, 64)

		if cs.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000) {
			t.Fatalf("got a plan with %.1fs left and the head analysis pending, want a wait", float64(9000-100)/50)
		}
		player.HookPreCacheStoreMu.Lock()
		(*player.HookPreCacheStore)[fmt.Sprintf("%s_%d", guildID, next.ID)].Analysis = steadyBeat(125)
		player.HookPreCacheStoreMu.Unlock()
		player.HookClearAnalysisPending(guildID, next.ID)

		cs.HookPlan(player.GetPlayer(guildID), endState, 150, 0, fade, false, 128000)
		inputs := ""
		for _, line := range strings.Split(readLog(), "\n") {
			if strings.Contains(line, "recipe") {
				inputs = line
			}
		}
		if !strings.Contains(inputs, "bpmA=128.0 bpmB=125.0") {
			t.Errorf("got plan inputs %q, want both BPMs once the analysis arrived", inputs)
		}
	})

	t.Run("another song pending", func(t *testing.T) {
		const guildID = "placewaitother"
		cs, endState, fade, _ := setup(t, guildID, 0, 64)

		if !cs.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000) {
			t.Error("got no plan with only the playing song's analysis pending, want one")
		}
	})

	t.Run("logs once", func(t *testing.T) {
		const guildID = "placewaitlogs"
		readLog := logtest.Capture(t)
		cs, endState, fade, next := setup(t, guildID, 1, 64)

		for range 3 {
			cs.HookPlan(player.GetPlayer(guildID), endState, 100, 0, fade, false, 128000)
		}
		for range 2 {
			cs.HookPlan(player.GetPlayer(guildID), endState, 8950, 0, fade, false, 128000)
		}

		log := readLog()
		if got := countLogLines(log, "waiting for the head analysis of "+next.Title); got != 1 {
			t.Errorf("got %d waiting lines over 3 plans, want 1", got)
		}
		if got := countLogLines(log, "planning without the head analysis of "+next.Title); got != 1 {
			t.Errorf("got %d planning-without lines over 2 plans, want 1", got)
		}
	})

	t.Run("outro longer than the overlap", func(t *testing.T) {
		const guildID = "placewaitoutro"
		cs, endState, fade, _ := setup(t, guildID, 1, 48)

		if cs.HookPlan(player.GetPlayer(guildID), endState, 7850, 0, fade, false, 128000) {
			t.Error("got a plan with 23.0s left before the outro deadline, want a wait")
		}
		if !cs.HookPlan(player.GetPlayer(guildID), endState, 7900, 0, fade, false, 128000) {
			t.Error("got no plan at the outro deadline, want one")
		}
	})
}
