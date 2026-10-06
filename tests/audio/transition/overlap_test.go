package transition_test

import (
	"fmt"
	"math"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
)

func steadyAnalysis(bpm float64, tonic int) *analysis.TrackAnalysis {
	track := &analysis.TrackAnalysis{
		BPM:           bpm,
		PeriodSec:     60 / bpm,
		FirstBeat:     0.3,
		Duration:      75,
		Tonic:         tonic,
		KeyConfidence: 0.5,
		BeatStrength:  0.6,
	}
	measureBars(track)
	return track
}

func measureBars(track *analysis.TrackAnalysis) {
	track.BarOffsets = make([]float64, int(track.Duration/track.BarLength()))
}

func outgoingTrack(duration, bpm float64, tonic int) transition.Track {
	tail := steadyAnalysis(bpm, tonic)
	tail.Offset = duration - 90
	tail.Duration = 90
	tail.FirstBeat = 0.2
	tail.DownbeatPhase = 1
	measureBars(tail)
	return transition.Track{URL: "https://example.com/a", Duration: duration, End: duration, Analysis: tail}
}

func incomingTrack(duration, bpm float64, tonic int) transition.Track {
	return transition.Track{URL: "https://example.com/b", Duration: duration, Analysis: steadyAnalysis(bpm, tonic)}
}

func beatPair() *transition.Pair {
	return &transition.Pair{From: outgoingTrack(200, 128, 0), To: incomingTrack(210, 124, 7), MaxBeats: 64}
}

func onGrid(position, origin, length float64) bool {
	bars := (position - origin) / length
	return math.Abs(bars-math.Round(bars)) < 1e-6
}

func TestCompatiblePairBeatmatchesOnTheDownbeats(t *testing.T) {
	pair := beatPair()
	overlap, ok := transition.SelectOverlap(pair)
	if !ok || !overlap.Beatmatched {
		t.Fatalf("got %s, want a beatmatched overlap", overlap)
	}

	barA := 4 * 60 / 128.0
	a := pair.From.Analysis
	if !onGrid(overlap.StartA, a.Offset+a.FirstBeat+a.PeriodSec, barA) {
		t.Errorf("startA %.3f is off the outgoing downbeats", overlap.StartA)
	}
	if !onGrid(overlap.StartB, 0.3, barA*overlap.SpeedB) {
		t.Errorf("startB %.3f is off the incoming downbeats", overlap.StartB)
	}
	if math.Abs(overlap.SpeedB-128.0/124) > 1e-9 {
		t.Errorf("speedB = %.4f, want %.4f to match the outgoing tempo", overlap.SpeedB, 128.0/124)
	}
	if math.Abs(overlap.Length-float64(overlap.Bars)*barA) > 1e-9 {
		t.Errorf("length %.3f, want %d bars at the outgoing tempo", overlap.Length, overlap.Bars)
	}
	if end := overlap.StartA + overlap.Length; end > 200 || end < 200-barA {
		t.Errorf("overlap ends at %.3f, want within the last bar before the end at 200", end)
	}
	if overlap.Bars != 8 {
		t.Errorf("bars = %d, want 8: the full-score window beats a 16-bar one that must start before the last 15%%", overlap.Bars)
	}
	if overlap.StartB > 1 {
		t.Errorf("startB = %.3f, want the first incoming downbeat", overlap.StartB)
	}
}

func TestWindowsStartOnTheMeasuredDownbeats(t *testing.T) {
	plain, _ := transition.SelectOverlap(beatPair())
	pair := beatPair()
	for bar := range pair.From.Analysis.BarOffsets {
		pair.From.Analysis.BarOffsets[bar] = 0.012
	}
	for bar := range pair.To.Analysis.BarOffsets {
		pair.To.Analysis.BarOffsets[bar] = -0.008
	}

	shifted, _ := transition.SelectOverlap(pair)
	if math.Abs(shifted.StartA-plain.StartA-0.012) > 1e-9 || math.Abs(shifted.StartB-plain.StartB+0.008) > 1e-9 {
		t.Errorf("starts moved by %.4f and %.4f, want the measured +0.012 and -0.008", shifted.StartA-plain.StartA, shifted.StartB-plain.StartB)
	}
}

func TestOneUnsteadyStretchOnlyRulesOutItsOwnWindows(t *testing.T) {
	plain, _ := transition.SelectOverlap(beatPair())
	pair := beatPair()
	a := pair.From.Analysis
	lastBar := int((plain.StartA - a.FirstDownbeat()) / a.BarLength())
	a.BarOffsets[lastBar+3] = 0.05

	overlap, ok := transition.SelectOverlap(pair)
	if !ok || !overlap.Beatmatched {
		t.Fatalf("got %s, want a beatmatched overlap from the steady bars", overlap)
	}
	if overlap.StartA+overlap.Length > plain.StartA+3*a.BarLength()+1e-9 {
		t.Errorf("overlap %s still covers the unsteady bar after %.3f", overlap, plain.StartA+3*a.BarLength())
	}
}

func TestClashingKeysKeepTheOverlapShort(t *testing.T) {
	for name, tonic := range map[string]int{"two numbers away 10B": 2, "far away 2B": 6} {
		pair := beatPair()
		pair.To = incomingTrack(210, 124, tonic)

		if overlap, _ := transition.SelectOverlap(pair); !overlap.Beatmatched || overlap.Bars != 4 {
			t.Errorf("%s gave %s, want a 4-bar beatmatched overlap for a key outside the compatible tiers", name, overlap)
		}
	}
}

func TestIncomingWindowMustSitEarlyInAShortSong(t *testing.T) {
	pair := beatPair()
	pair.From = outgoingTrack(600, 128, 0)
	pair.To = incomingTrack(120, 124, 7)
	pair.To.Analysis.FirstBeat = 35

	if overlap, _ := transition.SelectOverlap(pair); overlap.Beatmatched {
		t.Errorf("got %s, want a fallback: every incoming window ends past a quarter of the 120s song", overlap)
	}
}

func TestFourBarOverlapsDrawFromTheLongTable(t *testing.T) {
	short := map[int]bool{1: true, 10: true, 19: true}
	outsideShort := false
	for index := 0; index < 100; index++ {
		pair := beatPair()
		pair.MaxBeats = 16
		pair.From.URL = fmt.Sprintf("https://example.com/%d", index)
		overlap, _ := transition.SelectOverlap(pair)
		if overlap.Bars != 4 {
			t.Fatalf("got %s, want 4 bars under a 16-beat cap", overlap)
		}
		if !short[overlap.Preset] {
			outsideShort = true
		}
	}
	if !outsideShort {
		t.Error("100 four-bar overlaps only used the 2-bar presets 1, 10 and 19")
	}
}

func TestBeatSettingCapsTheOverlap(t *testing.T) {
	pair := beatPair()
	pair.MaxBeats = 16

	if overlap, _ := transition.SelectOverlap(pair); overlap.Bars != 4 {
		t.Errorf("got %s, want at most 4 bars for a 16-beat cap", overlap)
	}
}

func TestHalfTimeTrackFoldsOntoTheTempo(t *testing.T) {
	pair := beatPair()
	pair.To = incomingTrack(210, 64, 7)

	overlap, _ := transition.SelectOverlap(pair)
	if !overlap.Beatmatched || math.Abs(overlap.SpeedB-1) > 1e-9 {
		t.Errorf("got %s, want a beatmatched overlap at the folded tempo", overlap)
	}
}

func TestUnmatchablePairsFallBackToAShortFade(t *testing.T) {
	cases := map[string]func(*transition.Pair){
		"weak beat": func(p *transition.Pair) { p.To.Analysis.BeatStrength = 0.1 },
		"unsteady tempo": func(p *transition.Pair) {
			for bar := range p.From.Analysis.BarOffsets {
				p.From.Analysis.BarOffsets[bar] = 0.03 * float64(bar%2)
			}
		},
		"no measured bars": func(p *transition.Pair) { p.To.Analysis.BarOffsets = nil },
		"tempo too far":    func(p *transition.Pair) { p.To = incomingTrack(210, 112, 7) },
		"no analysis":      func(p *transition.Pair) { p.From.Analysis = nil },
		"no beat grid":     func(p *transition.Pair) { p.To.Analysis.PeriodSec = 0 },
		"no room to beat":  func(p *transition.Pair) { p.MaxBeats = 4 },
	}
	for name, change := range cases {
		pair := beatPair()
		change(pair)
		overlap, ok := transition.SelectOverlap(pair)
		if !ok || overlap.Beatmatched || overlap.Preset != transition.FadePreset {
			t.Errorf("%s gave %s, want the Fade fallback", name, overlap)
			continue
		}
		if overlap.Length != 5 || overlap.StartA != 195 || overlap.StartB != 0 || overlap.SpeedB != 1 {
			t.Errorf("%s gave %s, want the last 5s over the first 5s at normal speed", name, overlap)
		}
	}
}

func TestFallbackShrinksForShortTracks(t *testing.T) {
	cases := []struct {
		durationA, durationB float64
		length               float64
		ok                   bool
	}{
		{40, 300, 5, true},
		{300, 36, 3, true},
		{36, 36, 3, true},
		{30, 300, 0, false},
	}
	for _, c := range cases {
		pair := &transition.Pair{
			From: transition.Track{URL: "a", Duration: c.durationA, End: c.durationA},
			To:   transition.Track{URL: "b", Duration: c.durationB},
		}
		overlap, ok := transition.SelectOverlap(pair)
		if ok != c.ok || (ok && math.Abs(overlap.Length-c.length) > 1e-9) {
			t.Errorf("durations %.0f/%.0f gave %s (ok %t), want length %.0f (ok %t)", c.durationA, c.durationB, overlap, ok, c.length, c.ok)
		}
	}
}

func TestUnknownDurationsFallBackToWhatTheAnalysisKnows(t *testing.T) {
	pair := beatPair()
	pair.From.Duration = 0
	pair.To.Duration = 0

	overlap, ok := transition.SelectOverlap(pair)
	if !ok {
		t.Fatal("no overlap without durations, want the outgoing end and the incoming analysis to stand in")
	}
	if end := overlap.StartA + overlap.Length; end > 200+1e-9 {
		t.Errorf("overlap ends at %.3f, want it inside the outgoing end at 200", end)
	}
}

func TestLatePlanningSkipsWindowsThatAlreadyStarted(t *testing.T) {
	pair := beatPair()
	pair.EarliestStart = 196

	overlap, ok := transition.SelectOverlap(pair)
	if !ok || overlap.Beatmatched || overlap.StartA != 196 || overlap.Length != 4 {
		t.Errorf("got %s, want a fallback starting at 196 for the 4s that are left", overlap)
	}
}

func TestSelectionIsRepeatable(t *testing.T) {
	first, _ := transition.SelectOverlap(beatPair())
	second, _ := transition.SelectOverlap(beatPair())

	if first != second {
		t.Errorf("the same pair gave %s and then %s, want the panel and the player to agree", first, second)
	}
}

func TestPresetsComeFromTheTwoTables(t *testing.T) {
	long := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 8: true, 9: true, 10: true, 17: true, 18: true, 19: true}
	short := map[int]bool{1: true, 10: true, 19: true}
	seenLong := map[int]bool{}
	seenShort := map[int]bool{}

	for index := 0; index < 400; index++ {
		pair := beatPair()
		pair.From.URL = fmt.Sprintf("https://example.com/%d", index)
		overlap, _ := transition.SelectOverlap(pair)
		if !long[overlap.Preset] {
			t.Fatalf("%d-bar overlap picked preset %d, outside the long table", overlap.Bars, overlap.Preset)
		}
		seenLong[overlap.Preset] = true

		pair.MaxBeats = 8
		overlap, _ = transition.SelectOverlap(pair)
		if overlap.Bars != 2 || !short[overlap.Preset] {
			t.Fatalf("got %s, want a 2-bar overlap from the short table", overlap)
		}
		seenShort[overlap.Preset] = true
	}
	if len(seenLong) != len(long) || len(seenShort) != len(short) {
		t.Errorf("400 pairs used %d long and %d short presets, want every preset to come up", len(seenLong), len(seenShort))
	}
}

func TestPresetRecipesMatchTheirStyles(t *testing.T) {
	cases := map[int]*transition.Recipe{
		1:  combinedRecipe(volumeStyle("smooth"), eqStyle("center_bass_swap")),
		2:  combinedRecipe(volumeStyle("overlap"), eqStyle("three_band_fade")),
		3:  combinedRecipe(volumeStyle("overlap"), eqStyle("end_bass_swap"), filterStyle("lowpass_in_highpass_out")),
		4:  combinedRecipe(volumeStyle("fadein_fadeout"), eqStyle("center_bass_swap"), filterStyle("highpass_in_out")),
		9:  combinedRecipe(volumeStyle("cutin_fadeout"), eqStyle("center_bass_swap"), filterStyle("highpass_in")),
		18: combinedRecipe(volumeStyle("fadein_cutout"), eqStyle("center_bass_swap"), filterStyle("lowpass_out")),
		6:  combinedRecipe(),
	}
	for preset, want := range cases {
		if got := transition.PresetRecipe(preset); *got != *want {
			t.Errorf("preset %d = %s, want %s", preset, got, want)
		}
	}
}

func TestForcedLengthPicksThatBarCount(t *testing.T) {
	pair := beatPair()
	pair.MaxBeats = 16
	pair.Settings.Bars = 8

	overlap, ok := transition.SelectOverlap(pair)
	if !ok || !overlap.Beatmatched || overlap.Bars != 8 {
		t.Errorf("got %s, want 8 bars even over a 16-beat cap", overlap)
	}
}

func TestBeatmatchOffFallsBackToTheFade(t *testing.T) {
	pair := beatPair()
	pair.Settings.Beatmatch = transition.BeatmatchOff

	overlap, ok := transition.SelectOverlap(pair)
	if !ok || overlap.Beatmatched || overlap.Preset != transition.FadePreset || overlap.Length != 5 {
		t.Errorf("got %s, want the 5s Fade for a pair that could beatmatch", overlap)
	}
}

func TestBeatmatchOnOverridesTheBeatAndSteadinessChecks(t *testing.T) {
	pair := beatPair()
	pair.To.Analysis.BeatStrength = 0.1
	for bar := range pair.From.Analysis.BarOffsets {
		pair.From.Analysis.BarOffsets[bar] = 0.03 * float64(bar%2)
	}
	if overlap, _ := transition.SelectOverlap(pair); overlap.Beatmatched {
		t.Fatalf("got %s before forcing, want the weak, unsteady pair to fall back", overlap)
	}

	pair.Settings.Beatmatch = transition.BeatmatchOn
	if overlap, ok := transition.SelectOverlap(pair); !ok || !overlap.Beatmatched {
		t.Errorf("got %s, want a forced beatmatch", overlap)
	}

	pair.To = incomingTrack(210, 112, 7)
	if overlap, _ := transition.SelectOverlap(pair); overlap.Beatmatched {
		t.Errorf("got %s, want no beatmatch beyond the 10%% speed change even when forced", overlap)
	}
}

func TestPresetOverrideReplacesTheHashedPick(t *testing.T) {
	pair := beatPair()
	pair.Settings.Preset = 11
	if overlap, _ := transition.SelectOverlap(pair); !overlap.Beatmatched || overlap.Preset != 11 {
		t.Errorf("got %s, want the chosen preset 11 on the beatmatched overlap", overlap)
	}

	pair.Settings.Beatmatch = transition.BeatmatchOff
	if overlap, _ := transition.SelectOverlap(pair); overlap.Beatmatched || overlap.Preset != 11 {
		t.Errorf("got %s, want the chosen preset 11 instead of the Fade", overlap)
	}
}

func TestForcedLengthStretchesTheFallback(t *testing.T) {
	cases := []struct {
		name   string
		change func(*transition.Pair)
		length float64
	}{
		{"outgoing tempo", func(p *transition.Pair) {}, 4 * 4 * 60 / 128.0},
		{"no analysis", func(p *transition.Pair) { p.From.Analysis = nil }, 8},
		{"short song", func(p *transition.Pair) { p.From = outgoingTrack(40, 128, 0) }, 5},
	}
	for _, c := range cases {
		pair := beatPair()
		pair.Settings = transition.Settings{Bars: 4, Beatmatch: transition.BeatmatchOff}
		c.change(pair)
		overlap, ok := transition.SelectOverlap(pair)
		if !ok || math.Abs(overlap.Length-c.length) > 1e-9 || overlap.Bars != 4 {
			t.Errorf("%s gave %s, want a %.3fs fallback shaped as 4 bars", c.name, overlap, c.length)
		}
	}
}
