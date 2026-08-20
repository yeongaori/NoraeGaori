//go:build automixcheck

package player

import (
	"fmt"
	"math"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
)

func checkVolumeStyles(c *checkCollector) {
	styles := []transition.VolumeStyle{
		transition.VolumeSmoothCrossfade, transition.VolumeOverlap, transition.VolumeFadeInFadeOut,
		transition.VolumeCutInFadeOut, transition.VolumeFadeInCutOut,
	}

	allBounded := true
	detail := ""
	for _, style := range styles {
		recipe := transition.DefaultRecipe()
		recipe.Volume = style
		processor := transition.NewProcessor(recipe, 200, 0.5)
		for step := 0; step <= 100; step++ {
			progress := float64(step) / 100
			aGain, bGain := processor.Gains(progress)
			if math.IsNaN(aGain) || math.IsNaN(bGain) || aGain < 0 || bGain < 0 || aGain > 1.01 || bGain > 1.01 {
				allBounded = false
				detail = fmt.Sprintf("%s at p=%.2f gave a=%.3f b=%.3f", style, progress, aGain, bGain)
				break
			}
		}
		if !allBounded {
			break
		}
	}
	if allBounded {
		detail = "all five volume styles stayed within [0,1] across the window"
	}
	c.add("volume style gains bounded", allBounded, "%s", detail)

	smooth := transition.NewProcessor(transition.DefaultRecipe(), 200, 0.5)
	equalPower := true
	worst := 0.0
	for step := 0; step <= 100; step++ {
		progress := float64(step) / 100
		aGain, bGain := smooth.Gains(progress)
		sum := aGain*aGain + bGain*bGain
		if math.Abs(sum-1) > worst {
			worst = math.Abs(sum - 1)
		}
		if math.Abs(sum-1) > 0.001 {
			equalPower = false
		}
	}
	c.add("smooth crossfade is equal power", equalPower, "worst deviation %.6f", worst)

	directions := map[transition.VolumeStyle][2]bool{
		transition.VolumeSmoothCrossfade: {true, true},
		transition.VolumeFadeInFadeOut:   {true, true},
		transition.VolumeCutInFadeOut:    {true, false},
		transition.VolumeFadeInCutOut:    {false, true},
	}
	monotonic := true
	detail = ""
	for style, want := range directions {
		recipe := transition.DefaultRecipe()
		recipe.Volume = style
		processor := transition.NewProcessor(recipe, 200, 0.5)
		previousA, previousB := processor.Gains(0)
		for step := 1; step <= 100; step++ {
			aGain, bGain := processor.Gains(float64(step) / 100)
			if want[0] && aGain > previousA+1e-9 {
				monotonic = false
				detail = fmt.Sprintf("%s A gain rose at p=%.2f", style, float64(step)/100)
			}
			if want[1] && bGain < previousB-1e-9 {
				monotonic = false
				detail = fmt.Sprintf("%s B gain fell at p=%.2f", style, float64(step)/100)
			}
			previousA, previousB = aGain, bGain
		}
	}
	if monotonic {
		detail = "outgoing gains never rise, incoming gains never fall"
	}
	c.add("volume style monotonicity", monotonic, "%s", detail)

	flat := transition.NewProcessor(transition.DefaultRecipe(), 200, 0.5)
	flat.SetFlatGains(true)
	aGain, bGain := flat.Gains(0.5)
	c.add("automix without crossfade keeps flat gains", aGain == 1 && bGain == 1,
		"a=%.2f b=%.2f", aGain, bGain)
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

func checkRecipeMatrix(c *checkCollector) {
	volumes := []transition.VolumeStyle{transition.VolumeSmoothCrossfade, transition.VolumeOverlap, transition.VolumeFadeInFadeOut, transition.VolumeCutInFadeOut, transition.VolumeFadeInCutOut}
	eqs := []transition.EQStyle{transition.EQNone, transition.EQCenterBassSwap, transition.EQEndBassSwap, transition.EQStartBassSwap, transition.EQThreeBandFade, transition.EQQuickBass}
	filters := []transition.FilterStyle{transition.FilterNone, transition.FilterLowPassOut, transition.FilterLowPassIn, transition.FilterLowPassInOut, transition.FilterLowPassInHighPassOut}
	effects := []transition.EffectStyle{transition.EffectNone, transition.EffectReverbOutCenter, transition.EffectReverbCutEnd, transition.EffectReverbOutEnd, transition.EffectEchoHalfCutEnd}
	loops := []transition.LoopStyle{transition.LoopNone, transition.LoopOneBeat, transition.LoopTwoBeats, transition.LoopFourBeats, transition.LoopEightBeats}

	combinations := 0
	failures := 0
	worstJump := 0.0
	worstPeak := 0.0
	firstFailure := ""

	for _, volume := range volumes {
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
						if !finite || jump > 8000 {
							failures++
							if firstFailure == "" {
								firstFailure = fmt.Sprintf("%s (finite=%v jump=%.0f)", recipe, finite, jump)
							}
						}
					}
				}
			}
		}
	}

	detail := fmt.Sprintf("%d recipe combinations, worst sample jump %.0f, worst peak %.0f", combinations, worstJump, worstPeak)
	if failures > 0 {
		detail = fmt.Sprintf("%d/%d failed, first: %s", failures, combinations, firstFailure)
	}
	c.add("every recipe combination stays finite and click free", failures == 0, "%s", detail)
}

func checkLongWindows(c *checkCollector) {
	singles := []transition.Recipe{}
	for _, volume := range []transition.VolumeStyle{transition.VolumeSmoothCrossfade, transition.VolumeOverlap, transition.VolumeFadeInFadeOut, transition.VolumeCutInFadeOut, transition.VolumeFadeInCutOut} {
		recipe := transition.DefaultRecipe()
		recipe.Volume = volume
		singles = append(singles, recipe)
	}
	for _, eq := range []transition.EQStyle{transition.EQCenterBassSwap, transition.EQEndBassSwap, transition.EQStartBassSwap, transition.EQThreeBandFade, transition.EQQuickBass} {
		recipe := transition.DefaultRecipe()
		recipe.EQ = eq
		singles = append(singles, recipe)
	}
	for _, filter := range []transition.FilterStyle{transition.FilterLowPassOut, transition.FilterLowPassIn, transition.FilterLowPassInOut, transition.FilterLowPassInHighPassOut} {
		recipe := transition.DefaultRecipe()
		recipe.Filter = filter
		singles = append(singles, recipe)
	}
	for _, effect := range []transition.EffectStyle{transition.EffectReverbOutCenter, transition.EffectReverbCutEnd, transition.EffectReverbOutEnd, transition.EffectEchoHalfCutEnd} {
		recipe := transition.DefaultRecipe()
		recipe.Effect = effect
		singles = append(singles, recipe)
	}

	failures := 0
	worstJump := 0.0
	totalClipped := 0
	firstFailure := ""
	for _, recipe := range singles {
		finite, jump, _, clipped := runTransitionWindow(recipe, 400, 0.5)
		totalClipped += clipped
		if jump > worstJump {
			worstJump = jump
		}
		if !finite || jump > 8000 {
			failures++
			if firstFailure == "" {
				firstFailure = fmt.Sprintf("%s (finite=%v jump=%.0f)", recipe, finite, jump)
			}
		}
	}

	detail := fmt.Sprintf("%d single-style 8s windows, worst jump %.0f, clipped samples %d", len(singles), worstJump, totalClipped)
	if failures > 0 {
		detail = fmt.Sprintf("%d failed, first: %s", failures, firstFailure)
	}
	c.add("full length windows per style", failures == 0, "%s", detail)
}

func checkEdgeWindows(c *checkCollector) {
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

	failures := 0
	detail := ""
	for _, testCase := range cases {
		recipe := transition.Recipe{
			Volume: transition.VolumeCutInFadeOut,
			EQ:     transition.EQQuickBass,
			Filter: transition.FilterLowPassInHighPassOut,
			Effect: transition.EffectEchoHalfCutEnd,
			Loop:   transition.LoopFourBeats,
		}
		finite, jump, _, _ := runTransitionWindow(recipe, testCase.crossfadeFrames, testCase.periodSec)
		if !finite || math.IsNaN(jump) {
			failures++
			detail = testCase.name + " produced non-finite audio"
		}
	}
	if failures == 0 {
		detail = fmt.Sprintf("%d degenerate window configurations handled", len(cases))
	}
	c.add("degenerate transition windows", failures == 0, "%s", detail)

	processor := transition.NewProcessor(transition.DefaultRecipe(), 100, 0.5)
	loud := make([]int16, frameSize*channels)
	for i := range loud {
		if i%2 == 0 {
			loud[i] = 32767
		} else {
			loud[i] = -32768
		}
	}
	aBuf := processor.ProcessA(loud, 0.5)
	bBuf := processor.ProcessB(loud, 0.5)
	c.add("full scale input stays finite", bufferFinite(aBuf) && bufferFinite(bBuf),
		"peak a %.0f, peak b %.0f", bufferPeak(aBuf), bufferPeak(bBuf))

	empty := processor.ProcessA(nil, 0.5)
	c.add("nil frame is treated as silence", bufferPeak(empty) == 0, "peak %.1f", bufferPeak(empty))
}

func checkTails(c *checkCollector) {
	effects := []transition.EffectStyle{transition.EffectReverbOutCenter, transition.EffectReverbCutEnd, transition.EffectReverbOutEnd, transition.EffectEchoHalfCutEnd}

	failures := 0
	detail := ""
	for _, effect := range effects {
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
			failures++
			detail = fmt.Sprintf("%s produced no tail", effect)
			continue
		}

		frames := 0
		silent := make([]int16, frameSize*channels)
		for {
			for i := range silent {
				silent[i] = 0
			}
			more := tail.Apply(silent)
			frames++
			if frames > transition.EchoTailFrames+transition.ReverbTailFrames+10 {
				failures++
				detail = fmt.Sprintf("%s tail never finished", effect)
				break
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
			failures++
			detail = fmt.Sprintf("%s tail ended at peak %d", effect, lastPeak)
		}
	}
	if failures == 0 {
		detail = "all effect tails decayed and terminated within budget"
	}
	c.add("effect tails decay after handoff", failures == 0, "%s", detail)

	dry := transition.DefaultRecipe()
	processor := transition.NewProcessor(dry, 100, 0.5)
	c.add("no tail for effect free recipes", processor.MakeTail(1.0) == nil, "tail is nil as expected")

	var nilTail *transition.Tail
	frame := make([]int16, frameSize*channels)
	c.add("nil tail apply is safe", !nilTail.Apply(frame), "returned false without panic")
}

func checkLoopBuffer(c *checkCollector) {
	state := newCrossfadeState()
	state.loopFrames = 3

	frames := make([][]int16, 5)
	for i := range frames {
		frames[i] = make([]int16, 4)
		for j := range frames[i] {
			frames[i][j] = int16(i*10 + j)
		}
	}

	captured := []int16{}
	for i := 0; i < 3; i++ {
		out := state.loopFrame(frames[i])
		captured = append(captured, out[0])
	}
	repeated := []int16{}
	for i := 0; i < 7; i++ {
		out := state.loopFrame(frames[4])
		repeated = append(repeated, out[0])
	}

	expected := []int16{0, 10, 20, 0, 10, 20, 0}
	matches := len(repeated) == len(expected)
	for i := range expected {
		if !matches || repeated[i] != expected[i] {
			matches = false
			break
		}
	}
	c.add("loop buffer repeats captured frames", matches, "captured %v, replayed %v", captured, repeated)

	independent := true
	frames[0][0] = 999
	if state.loopBuffer[0][0] == 999 {
		independent = false
	}
	c.add("loop buffer copies source frames", independent, "source mutation did not leak into loop")

	empty := newCrossfadeState()
	empty.loopFrames = 2
	c.add("loop buffer tolerates nil input before fill", empty.loopFrame(nil) == nil, "returned nil")
}

func checkStyleNames(c *checkCollector) {
	categories := []string{"volume", "eq", "filter", "effect", "loop"}
	roundTrip := true
	detail := ""

	for _, category := range categories {
		for _, value := range transition.StyleValues(category) {
			if !transition.ValidStyle(category, value) {
				roundTrip = false
				detail = fmt.Sprintf("%s/%s rejected by validator", category, value)
			}
		}
	}
	if roundTrip {
		detail = "all advertised style values validate"
	}
	c.add("style catalogue validates", roundTrip, "%s", detail)

	c.add("unknown category rejected", transition.StyleValues("bogus") == nil &&
		!transition.ValidStyle("bogus", "smooth"), "bogus category returns nil and false")
	c.add("unknown style rejected", !transition.ValidStyle("eq", "super_bass"), "eq/super_bass rejected")

	names := map[string]string{
		transition.VolumeOverlap.String():              "overlap",
		transition.EQThreeBandFade.String():            "three_band_fade",
		transition.FilterLowPassInHighPassOut.String(): "lowpass_in_highpass_out",
		transition.EffectEchoHalfCutEnd.String():       "echo_half_cut_end",
		transition.LoopEightBeats.String():             "eight_beats",
	}
	namesOK := true
	for got, want := range names {
		if got != want {
			namesOK = false
		}
	}
	c.add("style names round trip", namesOK, "%v", names)

	base := transition.DefaultRecipe()
	overridden := transition.ApplyStyleOverrides(base, transition.StyleOverrides{
		Volume: "overlap",
		EQ:     transition.StyleAuto,
		Filter: "garbage",
		Effect: "reverb_cut_end",
		Loop:   "",
	})
	c.add("overrides apply only for known values",
		overridden.Volume == transition.VolumeOverlap &&
			overridden.EQ == transition.EQNone &&
			overridden.Filter == transition.FilterNone &&
			overridden.Effect == transition.EffectReverbCutEnd &&
			overridden.Loop == transition.LoopNone,
		"result %s", overridden)

	beats := transition.LoopBeatCount(transition.LoopOneBeat) == 1 && transition.LoopBeatCount(transition.LoopTwoBeats) == 2 &&
		transition.LoopBeatCount(transition.LoopFourBeats) == 4 && transition.LoopBeatCount(transition.LoopEightBeats) == 8 &&
		transition.LoopBeatCount(transition.LoopNone) == 0
	c.add("loop beat counts", beats, "1/2/4/8 beat mapping correct")
}

func checkSelector(c *checkCollector) {
	makeAnalysis := func(bpm float64, tonic int, minor bool, confidence float64) *analysis.TrackAnalysis {
		return &analysis.TrackAnalysis{
			BPM:           bpm,
			PeriodSec:     60 / bpm,
			Tonic:         tonic,
			Minor:         minor,
			KeyConfidence: confidence,
		}
	}

	c.add("nil analysis falls back to default",
		transition.SelectRecipe(nil, nil) == transition.DefaultRecipe() &&
			transition.SelectRecipe(makeAnalysis(128, 0, false, 0.5), nil) == transition.DefaultRecipe(),
		"missing analysis keeps the plain smooth crossfade")

	matchedHarmonic := transition.SelectRecipe(makeAnalysis(128, 0, false, 0.5), makeAnalysis(128, 7, false, 0.5))
	c.add("matched tempo and harmonic key blends",
		matchedHarmonic.Volume == transition.VolumeOverlap && matchedHarmonic.EQ == transition.EQThreeBandFade,
		"got %s", matchedHarmonic)

	matchedClashing := transition.SelectRecipe(makeAnalysis(128, 0, false, 0.5), makeAnalysis(128, 6, false, 0.5))
	c.add("matched tempo with clashing key uses filters",
		matchedClashing.Filter == transition.FilterLowPassInHighPassOut && matchedClashing.EQ == transition.EQCenterBassSwap,
		"got %s", matchedClashing)

	wideGap := transition.SelectRecipe(makeAnalysis(90, 0, false, 0.5), makeAnalysis(130, 6, false, 0.5))
	c.add("wide tempo gap uses loop and echo",
		wideGap.Loop == transition.LoopFourBeats && wideGap.Effect == transition.EffectEchoHalfCutEnd && wideGap.Volume == transition.VolumeFadeInCutOut,
		"got %s", wideGap)

	noGrid := transition.SelectRecipe(
		&analysis.TrackAnalysis{BPM: 90, KeyConfidence: 0.5},
		&analysis.TrackAnalysis{BPM: 130, KeyConfidence: 0.5},
	)
	c.add("wide gap without beat grid avoids loops",
		noGrid.Loop == transition.LoopNone && noGrid.Effect == transition.EffectReverbCutEnd,
		"got %s", noGrid)

	halfTime := transition.SelectRecipe(makeAnalysis(90, 0, false, 0.5), makeAnalysis(174, 7, false, 0.5))
	c.add("half-time pair is treated as tempo compatible",
		halfTime.Volume != transition.VolumeFadeInCutOut && halfTime.Effect != transition.EffectEchoHalfCutEnd,
		"got %s (delta %.4f)", halfTime, analysis.TempoDelta(90, 174))

	lowConfidence := transition.SelectRecipe(makeAnalysis(128, 0, false, 0.001), makeAnalysis(128, 7, false, 0.001))
	c.add("low key confidence is treated as unknown key",
		lowConfidence.Volume != transition.VolumeOverlap,
		"got %s", lowConfidence)

	zeroBPM := transition.SelectRecipe(&analysis.TrackAnalysis{BPM: 0}, &analysis.TrackAnalysis{BPM: 0})
	c.add("zero bpm falls back to default", zeroBPM == transition.DefaultRecipe(), "got %s", zeroBPM)
}
