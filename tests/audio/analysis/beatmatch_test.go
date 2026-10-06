package analysis_test

import (
	"math"
	"math/rand"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/analysis"
)

func analyzeClicks(t *testing.T, jitterSec float64) *analysis.TrackAnalysis {
	t.Helper()
	samples := audiotest.SynthesizeJitteredClickTrack(124, 40, analysis.SampleRate, 4, 0, jitterSec)
	track, err := analysis.AnalyzeTrackSamples(samples, analysis.SampleRate)
	if err != nil {
		t.Fatalf("click track failed: %v", err)
	}
	return track
}

func eightBarVariance(track *analysis.TrackAnalysis, bar int) (float64, bool) {
	start := track.FirstDownbeat() + float64(bar)*track.BarLength()
	return track.BarLengthVariance(start, start+8*track.BarLength())
}

func TestSteadyClicksAreBeatmatchable(t *testing.T) {
	track := analyzeClicks(t, 0)

	if track.BeatStrength < 0.5 {
		t.Errorf("beat strength %.3f, want a clear pulse above 0.5", track.BeatStrength)
	}
	if len(track.BarOffsets) < 16 {
		t.Fatalf("measured %d bars, want the 40s track covered", len(track.BarOffsets))
	}
	for bar := 0; bar+8 < len(track.BarOffsets); bar++ {
		if variance, ok := eightBarVariance(track, bar); !ok || variance >= 0.0005 {
			t.Errorf("8 bars from bar %d have variance %.6f s^2 (ok %t), want under the 0.0005 s^2 limit", bar, variance, ok)
		}
	}
	for bar, offset := range track.BarOffsets {
		if math.Abs(offset) > 0.015 {
			t.Errorf("bar %d sits %.4fs off the grid, want on it", bar, offset)
		}
	}
}

func TestLooseTimingFailsTheSteadinessCheck(t *testing.T) {
	track := analyzeClicks(t, 0.06)

	failing := 0
	windows := 0
	for bar := 0; bar+8 < len(track.BarOffsets); bar++ {
		windows++
		if variance, ok := eightBarVariance(track, bar); !ok || variance >= 0.0005 {
			failing++
		}
	}
	if windows == 0 || failing < windows*3/4 {
		t.Errorf("%d of %d 8-bar windows failed with 60ms of jitter, want nearly all of them", failing, windows)
	}
}

func TestBarOffsetsFollowASectionThatDriftsLate(t *testing.T) {
	early := audiotest.SynthesizeClickTrack(124, 20, analysis.SampleRate, 4, 0)
	late := audiotest.SynthesizeClickTrack(124, 20, analysis.SampleRate, 4, 0)
	gap := make([]float32, int(0.04*analysis.SampleRate))
	samples := append(append(early, gap...), late...)

	track, err := analysis.AnalyzeTrackSamples(samples, analysis.SampleRate)
	if err != nil {
		t.Fatal(err)
	}
	firstHalf := track.BarOffsets[2]
	secondHalf := track.BarOffsets[len(track.BarOffsets)-2]
	if shift := secondHalf - firstHalf; math.Abs(shift-0.04) > 0.012 {
		t.Errorf("late bars moved %.4fs against the early ones, want about the inserted 0.04s", shift)
	}
}

func TestBarVarianceNeedsMeasuredBars(t *testing.T) {
	track := analyzeClicks(t, 0)

	if _, ok := track.BarLengthVariance(track.FirstDownbeat()-track.BarLength(), track.FirstDownbeat()+4*track.BarLength()); ok {
		t.Error("a window starting before the first measured bar was accepted")
	}
	end := track.FirstDownbeat() + float64(len(track.BarOffsets))*track.BarLength()
	if _, ok := track.BarLengthVariance(end-2*track.BarLength(), end+2*track.BarLength()); ok {
		t.Error("a window running past the last measured bar was accepted")
	}
}

func TestNoiseHasNoBeatStrength(t *testing.T) {
	source := rand.New(rand.NewSource(5))
	samples := make([]float32, int(40*analysis.SampleRate))
	for i := range samples {
		envelope := 0.3 + 0.2*math.Sin(float64(i)/analysis.SampleRate*0.7)
		samples[i] = float32(source.NormFloat64() * envelope)
	}
	track, err := analysis.AnalyzeTrackSamples(samples, analysis.SampleRate)
	if err != nil {
		t.Skipf("noise was rejected outright: %v", err)
	}
	if track.BeatStrength >= 0.3 {
		t.Errorf("beat strength %.3f for noise, want it under the 0.3 beatmatch threshold", track.BeatStrength)
	}
}

func TestLeadSilenceShiftsTheBeatGridAndDuration(t *testing.T) {
	clicks := audiotest.SynthesizeClickTrack(124, 40, analysis.SampleRate, 4, 0)
	lead := int(1.5 * analysis.SampleRate)
	padded := append(make([]float32, lead), clicks...)

	plain, err := analysis.AnalyzeTrackSamples(clicks, analysis.SampleRate)
	if err != nil {
		t.Fatal(err)
	}
	shifted, err := analysis.AnalyzeAfterLead(padded, lead, analysis.SampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(shifted.FirstBeat-plain.FirstBeat-1.5) > 1e-9 || math.Abs(shifted.Duration-plain.Duration-1.5) > 1e-9 {
		t.Errorf("first beat %.3f and duration %.3f, want both 1.5s later than %.3f and %.3f", shifted.FirstBeat, shifted.Duration, plain.FirstBeat, plain.Duration)
	}
}

func keyed(tonic int, minor bool) *analysis.TrackAnalysis {
	return &analysis.TrackAnalysis{Tonic: tonic, Minor: minor, KeyConfidence: 0.5}
}

func TestKeyTiersFollowTheCamelotRules(t *testing.T) {
	cMajor := keyed(0, false)
	cases := []struct {
		name  string
		other *analysis.TrackAnalysis
		tier  int
	}{
		{"same key 8B", keyed(0, false), 0},
		{"next number 9B", keyed(7, false), 0},
		{"previous number 7B", keyed(5, false), 0},
		{"relative minor 8A", keyed(9, true), 1},
		{"other letter next number 9A", keyed(4, true), 2},
		{"two numbers away 10B", keyed(2, false), 3},
		{"far away 2B", keyed(6, false), 4},
	}
	for _, c := range cases {
		if got := analysis.KeyTier(cMajor, c.other); got != c.tier {
			t.Errorf("8B against %s = tier %d, want %d", c.name, got, c.tier)
		}
	}
}

func TestKeyTierKeepsTheWrapQuirk(t *testing.T) {
	eMajor, aMajor, bMajor := keyed(4, false), keyed(9, false), keyed(11, false)

	if got := analysis.KeyTier(eMajor, bMajor); got != 0 {
		t.Errorf("12B against 1B = tier %d, want 0 across the wrap", got)
	}
	if got := analysis.KeyTier(eMajor, aMajor); got != 4 {
		t.Errorf("12B against 11B = tier %d, want 4: (n+1) mod 12 never reaches 12", got)
	}
}

func TestUnknownKeysCountAsCompatible(t *testing.T) {
	unsure := keyed(6, false)
	unsure.KeyConfidence = 0.001

	if got := analysis.KeyTier(keyed(0, false), unsure); got != 0 {
		t.Errorf("low-confidence key = tier %d, want 0", got)
	}
	if got := analysis.KeyTier(nil, keyed(0, false)); got != 0 {
		t.Errorf("missing analysis = tier %d, want 0", got)
	}
}
