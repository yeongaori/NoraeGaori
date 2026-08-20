package player

import (
	"math"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
)

var allVolumeStyles = []transition.VolumeStyle{
	transition.VolumeSmoothCrossfade, transition.VolumeOverlap, transition.VolumeFadeInFadeOut,
	transition.VolumeCutInFadeOut, transition.VolumeFadeInCutOut,
}

func TestVolumeStyleGainsStayBounded(t *testing.T) {
	for _, style := range allVolumeStyles {
		t.Run(style.String(), func(t *testing.T) {
			recipe := transition.DefaultRecipe()
			recipe.Volume = style
			processor := transition.NewProcessor(recipe, 200, 0.5)

			for step := 0; step <= 100; step++ {
				progress := float64(step) / 100
				aGain, bGain := processor.Gains(progress)

				if math.IsNaN(aGain) || math.IsNaN(bGain) {
					t.Fatalf("p=%.2f gave a=%v b=%v, want finite gains", progress, aGain, bGain)
				}
				if aGain < 0 || aGain > 1.01 {
					t.Errorf("p=%.2f outgoing gain %.3f, want within [0, 1.01]", progress, aGain)
				}
				if bGain < 0 || bGain > 1.01 {
					t.Errorf("p=%.2f incoming gain %.3f, want within [0, 1.01]", progress, bGain)
				}
			}
		})
	}
}

func TestSmoothCrossfadeIsEqualPower(t *testing.T) {
	processor := transition.NewProcessor(transition.DefaultRecipe(), 200, 0.5)

	worst := 0.0
	for step := 0; step <= 100; step++ {
		progress := float64(step) / 100
		aGain, bGain := processor.Gains(progress)

		deviation := math.Abs(aGain*aGain + bGain*bGain - 1)
		if deviation > worst {
			worst = deviation
		}
		if deviation > 0.001 {
			t.Errorf("p=%.2f power sum deviates by %.6f, want at most 0.001", progress, deviation)
		}
	}
	t.Logf("worst deviation %.6f", worst)
}

func TestVolumeStyleMonotonicity(t *testing.T) {
	directions := map[transition.VolumeStyle]struct{ outgoingFalls, incomingRises bool }{
		transition.VolumeSmoothCrossfade: {true, true},
		transition.VolumeFadeInFadeOut:   {true, true},
		transition.VolumeCutInFadeOut:    {true, false},
		transition.VolumeFadeInCutOut:    {false, true},
	}

	for style, want := range directions {
		t.Run(style.String(), func(t *testing.T) {
			recipe := transition.DefaultRecipe()
			recipe.Volume = style
			processor := transition.NewProcessor(recipe, 200, 0.5)

			previousA, previousB := processor.Gains(0)
			for step := 1; step <= 100; step++ {
				progress := float64(step) / 100
				aGain, bGain := processor.Gains(progress)

				if want.outgoingFalls && aGain > previousA+1e-9 {
					t.Errorf("p=%.2f outgoing gain rose from %.4f to %.4f", progress, previousA, aGain)
				}
				if want.incomingRises && bGain < previousB-1e-9 {
					t.Errorf("p=%.2f incoming gain fell from %.4f to %.4f", progress, previousB, bGain)
				}
				previousA, previousB = aGain, bGain
			}
		})
	}
}

func TestAutoMixWithoutCrossfadeKeepsFlatGains(t *testing.T) {
	processor := transition.NewProcessor(transition.DefaultRecipe(), 200, 0.5)
	processor.SetFlatGains(true)

	aGain, bGain := processor.Gains(0.5)
	if aGain != 1 {
		t.Errorf("outgoing gain = %.2f, want 1", aGain)
	}
	if bGain != 1 {
		t.Errorf("incoming gain = %.2f, want 1", bGain)
	}
}

func runTransitionWindow(recipe transition.Recipe, crossfadeFrames int, periodSec float64) (bool, float64, float64, int) {
	processor := transition.NewProcessor(recipe, crossfadeFrames, periodSec)
	aTone := &toneGenerator{frequency: 220, amplitude: 8000}
	bTone := &toneGenerator{frequency: 660, amplitude: 8000}

	aFrame := make([]int16, frameSize*channels)
	bFrame := make([]int16, frameSize*channels)
	mixed := make([]int16, frameSize*channels)

	finite := true
	maxJump := 0.0
	maxPeak := 0.0
	clipped := 0
	previousSample := 0.0

	for frame := 0; frame < crossfadeFrames; frame++ {
		aTone.fill(aFrame)
		bTone.fill(bFrame)
		progress := float64(frame) / float64(crossfadeFrames)

		aBuf := processor.ProcessA(aFrame, progress)
		bBuf := processor.ProcessB(bFrame, progress)
		if !bufferFinite(aBuf) || !bufferFinite(bBuf) {
			finite = false
			break
		}
		processor.ApplyGains(aBuf, bBuf, progress, 1.0)

		for i := range mixed {
			sample := aBuf[i] + bBuf[i]
			if math.Abs(sample) > maxPeak {
				maxPeak = math.Abs(sample)
			}
			if sample > 32767 || sample < -32768 {
				clipped++
			}
			if sample > 32767 {
				sample = 32767
			} else if sample < -32768 {
				sample = -32768
			}
			mixed[i] = int16(sample)

			if i%channels == 0 {
				jump := math.Abs(sample - previousSample)
				if jump > maxJump {
					maxJump = jump
				}
				previousSample = sample
			}
		}
	}

	return finite, maxJump, maxPeak, clipped
}

func TestEveryRecipeCombinationStaysFiniteAndClickFree(t *testing.T) {
	eqs := []transition.EQStyle{transition.EQNone, transition.EQCenterBassSwap, transition.EQEndBassSwap, transition.EQStartBassSwap, transition.EQThreeBandFade, transition.EQQuickBass}
	filters := []transition.FilterStyle{transition.FilterNone, transition.FilterLowPassOut, transition.FilterLowPassIn, transition.FilterLowPassInOut, transition.FilterLowPassInHighPassOut}
	effects := []transition.EffectStyle{transition.EffectNone, transition.EffectReverbOutCenter, transition.EffectReverbCutEnd, transition.EffectReverbOutEnd, transition.EffectEchoHalfCutEnd}
	loops := []transition.LoopStyle{transition.LoopNone, transition.LoopOneBeat, transition.LoopTwoBeats, transition.LoopFourBeats, transition.LoopEightBeats}

	combinations := 0
	worstJump := 0.0
	worstPeak := 0.0

	for _, volume := range allVolumeStyles {
		for _, eq := range eqs {
			for _, filter := range filters {
				for _, effect := range effects {
					for _, loop := range loops {
						recipe := transition.Recipe{Volume: volume, EQ: eq, Filter: filter, Effect: effect, Loop: loop}
						finite, jump, peak, _ := runTransitionWindow(recipe, 12, 0.5)
						combinations++

						if jump > worstJump {
							worstJump = jump
						}
						if peak > worstPeak {
							worstPeak = peak
						}
						if !finite {
							t.Errorf("%s produced non-finite audio", recipe)
						}
						if jump > 8000 {
							t.Errorf("%s jumped %.0f between samples, want at most 8000", recipe, jump)
						}
					}
				}
			}
		}
	}

	t.Logf("%d recipe combinations, worst sample jump %.0f, worst peak %.0f", combinations, worstJump, worstPeak)
}

func singleStyleRecipes() []transition.Recipe {
	recipes := []transition.Recipe{}

	for _, volume := range allVolumeStyles {
		recipe := transition.DefaultRecipe()
		recipe.Volume = volume
		recipes = append(recipes, recipe)
	}
	for _, eq := range []transition.EQStyle{transition.EQCenterBassSwap, transition.EQEndBassSwap, transition.EQStartBassSwap, transition.EQThreeBandFade, transition.EQQuickBass} {
		recipe := transition.DefaultRecipe()
		recipe.EQ = eq
		recipes = append(recipes, recipe)
	}
	for _, filter := range []transition.FilterStyle{transition.FilterLowPassOut, transition.FilterLowPassIn, transition.FilterLowPassInOut, transition.FilterLowPassInHighPassOut} {
		recipe := transition.DefaultRecipe()
		recipe.Filter = filter
		recipes = append(recipes, recipe)
	}
	for _, effect := range []transition.EffectStyle{transition.EffectReverbOutCenter, transition.EffectReverbCutEnd, transition.EffectReverbOutEnd, transition.EffectEchoHalfCutEnd} {
		recipe := transition.DefaultRecipe()
		recipe.Effect = effect
		recipes = append(recipes, recipe)
	}

	return recipes
}

func TestFullLengthWindowsPerStyle(t *testing.T) {
	worstJump := 0.0
	totalClipped := 0

	for _, recipe := range singleStyleRecipes() {
		finite, jump, _, clipped := runTransitionWindow(recipe, 400, 0.5)
		totalClipped += clipped
		if jump > worstJump {
			worstJump = jump
		}

		if !finite {
			t.Errorf("%s produced non-finite audio", recipe)
		}
		if jump > 8000 {
			t.Errorf("%s jumped %.0f between samples, want at most 8000", recipe, jump)
		}
	}

	t.Logf("worst jump %.0f, clipped samples %d", worstJump, totalClipped)
}

func TestDegenerateTransitionWindows(t *testing.T) {
	cases := []struct {
		name            string
		crossfadeFrames int
		periodSec       float64
	}{
		{"single frame window", 1, 0.5},
		{"two frame window", 2, 0.5},
		{"no beat grid", 200, 0},
		{"negative period", 200, -1},
		{"very slow beat", 200, 30},
		{"very fast beat", 200, 0.01},
	}

	recipe := transition.Recipe{
		Volume: transition.VolumeCutInFadeOut,
		EQ:     transition.EQQuickBass,
		Filter: transition.FilterLowPassInHighPassOut,
		Effect: transition.EffectEchoHalfCutEnd,
		Loop:   transition.LoopFourBeats,
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			finite, jump, _, _ := runTransitionWindow(recipe, testCase.crossfadeFrames, testCase.periodSec)

			if !finite {
				t.Error("produced non-finite audio")
			}
			if math.IsNaN(jump) {
				t.Error("produced a NaN sample jump")
			}
		})
	}
}

func TestFullScaleInputStaysFinite(t *testing.T) {
	processor := transition.NewProcessor(transition.DefaultRecipe(), 100, 0.5)

	loud := make([]int16, frameSize*channels)
	for i := range loud {
		if i%2 == 0 {
			loud[i] = 32767
		} else {
			loud[i] = -32768
		}
	}

	if aBuf := processor.ProcessA(loud, 0.5); !bufferFinite(aBuf) {
		t.Errorf("outgoing buffer is not finite (peak %.0f)", bufferPeak(aBuf))
	}
	if bBuf := processor.ProcessB(loud, 0.5); !bufferFinite(bBuf) {
		t.Errorf("incoming buffer is not finite (peak %.0f)", bufferPeak(bBuf))
	}
}

func TestNilFrameIsTreatedAsSilence(t *testing.T) {
	processor := transition.NewProcessor(transition.DefaultRecipe(), 100, 0.5)

	if peak := bufferPeak(processor.ProcessA(nil, 0.5)); peak != 0 {
		t.Errorf("peak = %.1f, want 0", peak)
	}
}

func TestEffectTailsDecayAfterHandoff(t *testing.T) {
	effects := []transition.EffectStyle{
		transition.EffectReverbOutCenter, transition.EffectReverbCutEnd,
		transition.EffectReverbOutEnd, transition.EffectEchoHalfCutEnd,
	}

	for _, effect := range effects {
		t.Run(effect.String(), func(t *testing.T) {
			recipe := transition.DefaultRecipe()
			recipe.Effect = effect
			processor := transition.NewProcessor(recipe, 100, 0.5)

			tone := &toneGenerator{frequency: 220, amplitude: 9000}
			frame := make([]int16, frameSize*channels)
			for i := 0; i < 100; i++ {
				tone.fill(frame)
				progress := float64(i) / 100
				aBuf := processor.ProcessA(frame, progress)
				bBuf := processor.ProcessB(frame, progress)
				processor.ApplyGains(aBuf, bBuf, progress, 1.0)
			}

			tail := processor.MakeTail(1.0)
			if tail == nil {
				t.Fatal("produced no tail")
			}

			budget := transition.EchoTailFrames + transition.ReverbTailFrames + 10
			silent := make([]int16, frameSize*channels)
			frames := 0
			for {
				for i := range silent {
					silent[i] = 0
				}
				more := tail.Apply(silent)
				frames++

				if frames > budget {
					t.Fatalf("tail never finished within %d frames", budget)
				}
				if !more {
					break
				}
			}

			lastPeak := 0
			for _, sample := range silent {
				if int(sample) > lastPeak {
					lastPeak = int(sample)
				}
			}
			if lastPeak > 1500 {
				t.Errorf("tail ended at peak %d, want at most 1500", lastPeak)
			}
		})
	}
}

func TestNoTailForEffectFreeRecipes(t *testing.T) {
	processor := transition.NewProcessor(transition.DefaultRecipe(), 100, 0.5)

	if tail := processor.MakeTail(1.0); tail != nil {
		t.Errorf("got a tail %v, want nil", tail)
	}
}

func TestNilTailApplyIsSafe(t *testing.T) {
	var tail *transition.Tail

	if tail.Apply(make([]int16, frameSize*channels)) {
		t.Error("a nil tail reported more audio to come")
	}
}

func TestLoopBufferRepeatsCapturedFrames(t *testing.T) {
	state := newCrossfadeState()
	state.loopFrames = 3

	frames := make([][]int16, 5)
	for i := range frames {
		frames[i] = make([]int16, 4)
		for j := range frames[i] {
			frames[i][j] = int16(i*10 + j)
		}
	}

	for i := 0; i < 3; i++ {
		state.loopFrame(frames[i])
	}

	want := []int16{0, 10, 20, 0, 10, 20, 0}
	for i, expected := range want {
		out := state.loopFrame(frames[4])
		if out[0] != expected {
			t.Errorf("replay %d = %d, want %d", i, out[0], expected)
		}
	}
}

func TestLoopBufferCopiesSourceFrames(t *testing.T) {
	state := newCrossfadeState()
	state.loopFrames = 3

	source := []int16{1, 2, 3, 4}
	state.loopFrame(source)
	source[0] = 999

	if state.loopBuffer[0][0] == 999 {
		t.Error("mutating the source frame leaked into the loop buffer")
	}
}

func TestLoopBufferToleratesNilInputBeforeFill(t *testing.T) {
	state := newCrossfadeState()
	state.loopFrames = 2

	if out := state.loopFrame(nil); out != nil {
		t.Errorf("got %v, want nil", out)
	}
}

func TestStyleCatalogueValidates(t *testing.T) {
	for _, category := range []string{"volume", "eq", "filter", "effect", "loop"} {
		t.Run(category, func(t *testing.T) {
			values := transition.StyleValues(category)
			if len(values) == 0 {
				t.Fatal("category advertises no style values")
			}
			for _, value := range values {
				if !transition.ValidStyle(category, value) {
					t.Errorf("advertised value %q is rejected by the validator", value)
				}
			}
		})
	}
}

func TestUnknownCategoryRejected(t *testing.T) {
	if values := transition.StyleValues("bogus"); values != nil {
		t.Errorf("got %v, want nil", values)
	}
	if transition.ValidStyle("bogus", "smooth") {
		t.Error("an unknown category accepted a style")
	}
}

func TestUnknownStyleRejected(t *testing.T) {
	if transition.ValidStyle("eq", "super_bass") {
		t.Error("eq accepted the unknown style super_bass")
	}
}

func TestStyleNamesRoundTrip(t *testing.T) {
	names := []struct {
		got  string
		want string
	}{
		{transition.VolumeOverlap.String(), "overlap"},
		{transition.EQThreeBandFade.String(), "three_band_fade"},
		{transition.FilterLowPassInHighPassOut.String(), "lowpass_in_highpass_out"},
		{transition.EffectEchoHalfCutEnd.String(), "echo_half_cut_end"},
		{transition.LoopEightBeats.String(), "eight_beats"},
	}

	for _, name := range names {
		if name.got != name.want {
			t.Errorf("got %q, want %q", name.got, name.want)
		}
	}
}

func TestOverridesApplyOnlyForKnownValues(t *testing.T) {
	overridden := transition.ApplyStyleOverrides(transition.DefaultRecipe(), transition.StyleOverrides{
		Volume: "overlap",
		EQ:     transition.StyleAuto,
		Filter: "garbage",
		Effect: "reverb_cut_end",
		Loop:   "",
	})

	if overridden.Volume != transition.VolumeOverlap {
		t.Errorf("volume = %s, want the known override overlap", overridden.Volume)
	}
	if overridden.EQ != transition.EQNone {
		t.Errorf("eq = %s, want none for an auto override", overridden.EQ)
	}
	if overridden.Filter != transition.FilterNone {
		t.Errorf("filter = %s, want none for an unknown override", overridden.Filter)
	}
	if overridden.Effect != transition.EffectReverbCutEnd {
		t.Errorf("effect = %s, want the known override reverb_cut_end", overridden.Effect)
	}
	if overridden.Loop != transition.LoopNone {
		t.Errorf("loop = %s, want none for an empty override", overridden.Loop)
	}
}

func TestLoopBeatCounts(t *testing.T) {
	counts := []struct {
		style transition.LoopStyle
		want  int
	}{
		{transition.LoopOneBeat, 1},
		{transition.LoopTwoBeats, 2},
		{transition.LoopFourBeats, 4},
		{transition.LoopEightBeats, 8},
		{transition.LoopNone, 0},
	}

	for _, count := range counts {
		if got := transition.LoopBeatCount(count.style); got != count.want {
			t.Errorf("%s = %d beats, want %d", count.style, got, count.want)
		}
	}
}

func analysisAt(bpm float64, tonic int, minor bool, confidence float64) *analysis.TrackAnalysis {
	return &analysis.TrackAnalysis{
		BPM:           bpm,
		PeriodSec:     60 / bpm,
		Tonic:         tonic,
		Minor:         minor,
		KeyConfidence: confidence,
	}
}

func TestNilAnalysisFallsBackToDefault(t *testing.T) {
	if got := transition.SelectRecipe(nil, nil); got != transition.DefaultRecipe() {
		t.Errorf("two nil analyses gave %s, want the default recipe", got)
	}
	if got := transition.SelectRecipe(analysisAt(128, 0, false, 0.5), nil); got != transition.DefaultRecipe() {
		t.Errorf("a nil incoming analysis gave %s, want the default recipe", got)
	}
}

func TestMatchedTempoAndHarmonicKeyBlends(t *testing.T) {
	got := transition.SelectRecipe(analysisAt(128, 0, false, 0.5), analysisAt(128, 7, false, 0.5))

	if got.Volume != transition.VolumeOverlap {
		t.Errorf("volume = %s, want overlap (got %s)", got.Volume, got)
	}
	if got.EQ != transition.EQThreeBandFade {
		t.Errorf("eq = %s, want three_band_fade (got %s)", got.EQ, got)
	}
}

func TestMatchedTempoWithClashingKeyUsesFilters(t *testing.T) {
	got := transition.SelectRecipe(analysisAt(128, 0, false, 0.5), analysisAt(128, 6, false, 0.5))

	if got.Filter != transition.FilterLowPassInHighPassOut {
		t.Errorf("filter = %s, want lowpass_in_highpass_out (got %s)", got.Filter, got)
	}
	if got.EQ != transition.EQCenterBassSwap {
		t.Errorf("eq = %s, want center_bass_swap (got %s)", got.EQ, got)
	}
}

func TestWideTempoGapUsesLoopAndEcho(t *testing.T) {
	got := transition.SelectRecipe(analysisAt(90, 0, false, 0.5), analysisAt(130, 6, false, 0.5))

	if got.Loop != transition.LoopFourBeats {
		t.Errorf("loop = %s, want four_beats (got %s)", got.Loop, got)
	}
	if got.Effect != transition.EffectEchoHalfCutEnd {
		t.Errorf("effect = %s, want echo_half_cut_end (got %s)", got.Effect, got)
	}
	if got.Volume != transition.VolumeFadeInCutOut {
		t.Errorf("volume = %s, want fade_in_cut_out (got %s)", got.Volume, got)
	}
}

func TestWideGapWithoutBeatGridAvoidsLoops(t *testing.T) {
	got := transition.SelectRecipe(
		&analysis.TrackAnalysis{BPM: 90, KeyConfidence: 0.5},
		&analysis.TrackAnalysis{BPM: 130, KeyConfidence: 0.5},
	)

	if got.Loop != transition.LoopNone {
		t.Errorf("loop = %s, want none without a beat grid (got %s)", got.Loop, got)
	}
	if got.Effect != transition.EffectReverbCutEnd {
		t.Errorf("effect = %s, want reverb_cut_end (got %s)", got.Effect, got)
	}
}

func TestHalfTimePairIsTreatedAsTempoCompatible(t *testing.T) {
	got := transition.SelectRecipe(analysisAt(90, 0, false, 0.5), analysisAt(174, 7, false, 0.5))

	if got.Volume == transition.VolumeFadeInCutOut {
		t.Errorf("volume = %s, want anything but the wide-gap fade_in_cut_out (delta %.4f)", got.Volume, analysis.TempoDelta(90, 174))
	}
	if got.Effect == transition.EffectEchoHalfCutEnd {
		t.Errorf("effect = %s, want anything but the wide-gap echo_half_cut_end (delta %.4f)", got.Effect, analysis.TempoDelta(90, 174))
	}
}

func TestLowKeyConfidenceIsTreatedAsUnknownKey(t *testing.T) {
	got := transition.SelectRecipe(analysisAt(128, 0, false, 0.001), analysisAt(128, 7, false, 0.001))

	if got.Volume == transition.VolumeOverlap {
		t.Errorf("volume = %s, want the harmonic blend to be withheld (got %s)", got.Volume, got)
	}
}

func TestZeroBPMFallsBackToDefault(t *testing.T) {
	got := transition.SelectRecipe(&analysis.TrackAnalysis{BPM: 0}, &analysis.TrackAnalysis{BPM: 0})

	if got != transition.DefaultRecipe() {
		t.Errorf("got %s, want the default recipe", got)
	}
}
