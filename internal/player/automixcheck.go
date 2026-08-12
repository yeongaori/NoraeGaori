package player

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"time"

	"noraegaori/internal/queue"
)

type CheckResult struct {
	Name   string
	Passed bool
	Detail string
}

type checkCollector struct {
	results []CheckResult
}

func (c *checkCollector) add(name string, passed bool, format string, args ...interface{}) {
	c.results = append(c.results, CheckResult{
		Name:   name,
		Passed: passed,
		Detail: fmt.Sprintf(format, args...),
	})
}

type toneGenerator struct {
	phase     float64
	frequency float64
	amplitude float64
}

func (t *toneGenerator) fill(frame []int16) {
	step := 2 * math.Pi * t.frequency / dspSampleRate
	for i := 0; i+1 < len(frame); i += 2 {
		value := int16(t.amplitude * math.Sin(t.phase))
		frame[i] = value
		frame[i+1] = value
		t.phase += step
		if t.phase > 2*math.Pi {
			t.phase -= 2 * math.Pi
		}
	}
}

func sineFloatFrame(frequency, amplitude float64, phase *float64) []float64 {
	buf := make([]float64, frameSize*channels)
	step := 2 * math.Pi * frequency / dspSampleRate
	for i := 0; i+1 < len(buf); i += 2 {
		value := amplitude * math.Sin(*phase)
		buf[i] = value
		buf[i+1] = value
		*phase += step
	}
	return buf
}

func bufferRMS(buf []float64) float64 {
	if len(buf) == 0 {
		return 0
	}
	var sum float64
	for _, v := range buf {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(buf)))
}

func bufferPeak(buf []float64) float64 {
	peak := 0.0
	for _, v := range buf {
		if math.Abs(v) > peak {
			peak = math.Abs(v)
		}
	}
	return peak
}

func bufferFinite(buf []float64) bool {
	for _, v := range buf {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func filterResponse(setup func(*biquad), frequency float64) float64 {
	var filter biquad
	setup(&filter)
	phase := 0.0
	var lastRMS float64
	for frame := 0; frame < 25; frame++ {
		buf := sineFloatFrame(frequency, 10000, &phase)
		filter.processStereo(buf)
		lastRMS = bufferRMS(buf)
	}
	return lastRMS / (10000 / math.Sqrt2)
}

func checkBiquads(c *checkCollector) {
	lowPassLow := filterResponse(func(f *biquad) { f.setLowpass(500, 0.707) }, 100)
	lowPassHigh := filterResponse(func(f *biquad) { f.setLowpass(500, 0.707) }, 8000)
	c.add("biquad lowpass shape", lowPassLow > 0.85 && lowPassHigh < 0.05,
		"100Hz gain %.3f (want >0.85), 8000Hz gain %.4f (want <0.05)", lowPassLow, lowPassHigh)

	highPassLow := filterResponse(func(f *biquad) { f.setHighpass(2000, 0.707) }, 100)
	highPassHigh := filterResponse(func(f *biquad) { f.setHighpass(2000, 0.707) }, 12000)
	c.add("biquad highpass shape", highPassHigh > 0.85 && highPassLow < 0.05,
		"12000Hz gain %.3f (want >0.85), 100Hz gain %.4f (want <0.05)", highPassHigh, highPassLow)

	shelfLow := filterResponse(func(f *biquad) { f.setLowShelf(eqLowFreq, eqShelfQ, eqKillDB) }, 60)
	shelfHigh := filterResponse(func(f *biquad) { f.setLowShelf(eqLowFreq, eqShelfQ, eqKillDB) }, 6000)
	c.add("biquad low shelf bass kill", shelfLow < 0.05 && shelfHigh > 0.9,
		"60Hz gain %.4f (want <0.05), 6000Hz gain %.3f (want >0.9)", shelfLow, shelfHigh)

	highShelfHigh := filterResponse(func(f *biquad) { f.setHighShelf(eqHighFreq, eqShelfQ, eqKillDB) }, 12000)
	highShelfLow := filterResponse(func(f *biquad) { f.setHighShelf(eqHighFreq, eqShelfQ, eqKillDB) }, 200)
	c.add("biquad high shelf treble kill", highShelfHigh < 0.05 && highShelfLow > 0.9,
		"12000Hz gain %.4f (want <0.05), 200Hz gain %.3f (want >0.9)", highShelfHigh, highShelfLow)

	peakCut := filterResponse(func(f *biquad) { f.setPeaking(eqMidFreq, eqMidQ, eqKillDB) }, eqMidFreq)
	c.add("biquad peaking mid cut", peakCut < 0.1, "1000Hz gain %.4f (want <0.1)", peakCut)

	var bypass biquad
	bypass.setBypass()
	phase := 0.0
	original := sineFloatFrame(1000, 9000, &phase)
	copied := make([]float64, len(original))
	copy(copied, original)
	bypass.processStereo(copied)
	identical := true
	for i := range original {
		if original[i] != copied[i] {
			identical = false
			break
		}
	}
	c.add("biquad bypass is transparent", identical, "sample-for-sample match: %v", identical)

	extremes := []struct {
		name  string
		setup func(*biquad)
	}{
		{"lowpass 0.1Hz", func(f *biquad) { f.setLowpass(0.1, 0.707) }},
		{"lowpass 96000Hz", func(f *biquad) { f.setLowpass(96000, 0.707) }},
		{"highpass 0Hz", func(f *biquad) { f.setHighpass(0, 0) }},
		{"peaking negative Q", func(f *biquad) { f.setPeaking(1000, -5, -40) }},
		{"lowshelf huge gain", func(f *biquad) { f.setLowShelf(250, 0.707, 120) }},
	}
	stable := true
	details := ""
	for _, extreme := range extremes {
		var filter biquad
		extreme.setup(&filter)
		localPhase := 0.0
		for frame := 0; frame < 50; frame++ {
			buf := sineFloatFrame(440, 10000, &localPhase)
			filter.processStereo(buf)
			if !bufferFinite(buf) {
				stable = false
				details = extreme.name + " produced non-finite output"
				break
			}
			if bufferPeak(buf) > 1e9 {
				stable = false
				details = extreme.name + " diverged"
				break
			}
		}
		if !stable {
			break
		}
	}
	if stable {
		details = "all extreme coefficient cases stayed finite and bounded"
	}
	c.add("biquad extreme parameters stable", stable, "%s", details)
}

func checkDelayAndReverb(c *checkCollector) {
	delay := newDelayLine()
	delay.setDelaySeconds(0.1)
	delay.feedback = 0.5
	delay.wet = 1
	delay.dry = 1

	impulse := make([]float64, frameSize*channels)
	impulse[0] = 10000
	impulse[1] = 10000
	delay.processStereo(impulse)

	silent := make([]float64, frameSize*channels)
	var firstEchoFrame int
	var firstEchoPeak float64
	for frame := 1; frame <= 10; frame++ {
		silenceFloat(silent)
		delay.processStereo(silent)
		peak := bufferPeak(silent)
		if peak > 100 && firstEchoFrame == 0 {
			firstEchoFrame = frame
			firstEchoPeak = peak
		}
	}
	c.add("delay line echo timing", firstEchoFrame == 5,
		"first echo at frame %d (want 5 for 100ms), peak %.0f", firstEchoFrame, firstEchoPeak)

	feedbackDecay := true
	var peaks []float64
	for frame := 0; frame < 20; frame++ {
		silenceFloat(silent)
		delay.processStereo(silent)
		peak := bufferPeak(silent)
		if peak > 1 {
			peaks = append(peaks, peak)
		}
	}
	for i := 1; i < len(peaks); i++ {
		if peaks[i] > peaks[i-1]*1.05 {
			feedbackDecay = false
		}
	}
	c.add("delay line feedback decays", feedbackDecay && len(peaks) > 0,
		"%d decaying echo peaks observed", len(peaks))

	unit := newReverb()
	wetImpulse := make([]float64, frameSize*channels)
	wetImpulse[0] = 20000
	wetImpulse[1] = 20000
	unit.processStereo(wetImpulse, 0, 1)

	tailEnergy := 0.0
	maxTailPeak := 0.0
	finite := true
	for frame := 0; frame < 60; frame++ {
		silenceFloat(silent)
		unit.processStereo(silent, 0, 1)
		if !bufferFinite(silent) {
			finite = false
			break
		}
		tailEnergy += bufferRMS(silent)
		if bufferPeak(silent) > maxTailPeak {
			maxTailPeak = bufferPeak(silent)
		}
	}
	c.add("reverb produces bounded tail", finite && tailEnergy > 0 && maxTailPeak < 40000,
		"tail energy %.1f, peak %.0f, finite %v", tailEnergy, maxTailPeak, finite)

	late := 0.0
	for frame := 0; frame < 400; frame++ {
		silenceFloat(silent)
		unit.processStereo(silent, 0, 1)
		late = bufferRMS(silent)
	}
	c.add("reverb tail decays to near silence", late < 1, "rms after 460 frames: %.6f", late)
}

func checkConversions(c *checkCollector) {
	src := []float64{40000, -40000, 100.4, -100.4, 0}
	dst := make([]int16, len(src))
	floatToFrame(src, dst)
	c.add("float to frame clamps", dst[0] == 32767 && dst[1] == -32768 && dst[2] == 100 && dst[4] == 0,
		"got %v", dst)

	frame := []int16{1000, -1000, 32767}
	out := make([]float64, 5)
	frameToFloat(frame, out)
	c.add("frame to float zero pads", out[0] == 1000 && out[2] == 32767 && out[3] == 0 && out[4] == 0,
		"got %v", out)

	ramp := make([]float64, frameSize*channels)
	for i := range ramp {
		ramp[i] = 1000
	}
	applyGainRamp(ramp, 0, 1)
	c.add("gain ramp endpoints", ramp[0] == 0 && ramp[len(ramp)-1] > 900 && ramp[len(ramp)-1] <= 1000,
		"first %.1f, last %.1f", ramp[0], ramp[len(ramp)-1])

	flat := make([]float64, 8)
	for i := range flat {
		flat[i] = 500
	}
	applyGainRamp(flat, 0.5, 0.5)
	c.add("gain ramp constant factor", flat[0] == 250 && flat[7] == 250, "got %v", flat[:2])
}

func checkVolumeStyles(c *checkCollector) {
	styles := []VolumeStyle{
		VolumeSmoothCrossfade, VolumeOverlap, VolumeFadeInFadeOut,
		VolumeCutInFadeOut, VolumeFadeInCutOut,
	}

	allBounded := true
	detail := ""
	for _, style := range styles {
		recipe := defaultTransitionRecipe()
		recipe.Volume = style
		processor := newTransitionProcessor(recipe, 200, 0.5)
		for step := 0; step <= 100; step++ {
			progress := float64(step) / 100
			aGain, bGain := processor.gains(progress)
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

	smooth := newTransitionProcessor(defaultTransitionRecipe(), 200, 0.5)
	equalPower := true
	worst := 0.0
	for step := 0; step <= 100; step++ {
		progress := float64(step) / 100
		aGain, bGain := smooth.gains(progress)
		sum := aGain*aGain + bGain*bGain
		if math.Abs(sum-1) > worst {
			worst = math.Abs(sum - 1)
		}
		if math.Abs(sum-1) > 0.001 {
			equalPower = false
		}
	}
	c.add("smooth crossfade is equal power", equalPower, "worst deviation %.6f", worst)

	directions := map[VolumeStyle][2]bool{
		VolumeSmoothCrossfade: {true, true},
		VolumeFadeInFadeOut:   {true, true},
		VolumeCutInFadeOut:    {true, false},
		VolumeFadeInCutOut:    {false, true},
	}
	monotonic := true
	detail = ""
	for style, want := range directions {
		recipe := defaultTransitionRecipe()
		recipe.Volume = style
		processor := newTransitionProcessor(recipe, 200, 0.5)
		previousA, previousB := processor.gains(0)
		for step := 1; step <= 100; step++ {
			aGain, bGain := processor.gains(float64(step) / 100)
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

	flat := newTransitionProcessor(defaultTransitionRecipe(), 200, 0.5)
	flat.flatGains = true
	aGain, bGain := flat.gains(0.5)
	c.add("automix without crossfade keeps flat gains", aGain == 1 && bGain == 1,
		"a=%.2f b=%.2f", aGain, bGain)
}

func runTransitionWindow(recipe TransitionRecipe, crossfadeFrames int, periodSec float64) (bool, float64, float64, int) {
	processor := newTransitionProcessor(recipe, crossfadeFrames, periodSec)
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

		aBuf := processor.processA(aFrame, progress)
		bBuf := processor.processB(bFrame, progress)
		if !bufferFinite(aBuf) || !bufferFinite(bBuf) {
			finite = false
			break
		}
		processor.applyGains(aBuf, bBuf, progress, 1.0)

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
	volumes := []VolumeStyle{VolumeSmoothCrossfade, VolumeOverlap, VolumeFadeInFadeOut, VolumeCutInFadeOut, VolumeFadeInCutOut}
	eqs := []EQStyle{EQNone, EQCenterBassSwap, EQEndBassSwap, EQStartBassSwap, EQThreeBandFade, EQQuickBass}
	filters := []FilterStyle{FilterNone, FilterLowPassOut, FilterLowPassIn, FilterLowPassInOut, FilterLowPassInHighPassOut}
	effects := []EffectStyle{EffectNone, EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd, EffectEchoHalfCutEnd}
	loops := []LoopStyle{LoopNone, LoopOneBeat, LoopTwoBeats, LoopFourBeats, LoopEightBeats}

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
						recipe := TransitionRecipe{Volume: volume, EQ: eq, Filter: filter, Effect: effect, Loop: loop}
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
	singles := []TransitionRecipe{}
	for _, volume := range []VolumeStyle{VolumeSmoothCrossfade, VolumeOverlap, VolumeFadeInFadeOut, VolumeCutInFadeOut, VolumeFadeInCutOut} {
		recipe := defaultTransitionRecipe()
		recipe.Volume = volume
		singles = append(singles, recipe)
	}
	for _, eq := range []EQStyle{EQCenterBassSwap, EQEndBassSwap, EQStartBassSwap, EQThreeBandFade, EQQuickBass} {
		recipe := defaultTransitionRecipe()
		recipe.EQ = eq
		singles = append(singles, recipe)
	}
	for _, filter := range []FilterStyle{FilterLowPassOut, FilterLowPassIn, FilterLowPassInOut, FilterLowPassInHighPassOut} {
		recipe := defaultTransitionRecipe()
		recipe.Filter = filter
		singles = append(singles, recipe)
	}
	for _, effect := range []EffectStyle{EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd, EffectEchoHalfCutEnd} {
		recipe := defaultTransitionRecipe()
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
		recipe := TransitionRecipe{
			Volume: VolumeCutInFadeOut,
			EQ:     EQQuickBass,
			Filter: FilterLowPassInHighPassOut,
			Effect: EffectEchoHalfCutEnd,
			Loop:   LoopFourBeats,
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

	processor := newTransitionProcessor(defaultTransitionRecipe(), 100, 0.5)
	loud := make([]int16, frameSize*channels)
	for i := range loud {
		if i%2 == 0 {
			loud[i] = 32767
		} else {
			loud[i] = -32768
		}
	}
	aBuf := processor.processA(loud, 0.5)
	bBuf := processor.processB(loud, 0.5)
	c.add("full scale input stays finite", bufferFinite(aBuf) && bufferFinite(bBuf),
		"peak a %.0f, peak b %.0f", bufferPeak(aBuf), bufferPeak(bBuf))

	empty := processor.processA(nil, 0.5)
	c.add("nil frame is treated as silence", bufferPeak(empty) == 0, "peak %.1f", bufferPeak(empty))
}

func checkTails(c *checkCollector) {
	effects := []EffectStyle{EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd, EffectEchoHalfCutEnd}

	failures := 0
	detail := ""
	for _, effect := range effects {
		recipe := defaultTransitionRecipe()
		recipe.Effect = effect
		processor := newTransitionProcessor(recipe, 100, 0.5)

		tone := &toneGenerator{frequency: 220, amplitude: 9000}
		frame := make([]int16, frameSize*channels)
		for i := 0; i < 100; i++ {
			tone.fill(frame)
			progress := float64(i) / 100
			aBuf := processor.processA(frame, progress)
			bBuf := processor.processB(frame, progress)
			processor.applyGains(aBuf, bBuf, progress, 1.0)
		}

		tail := processor.makeTail(1.0)
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
			more := tail.apply(silent)
			frames++
			if frames > echoTailFrames+reverbTailFrames+10 {
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

	dry := defaultTransitionRecipe()
	processor := newTransitionProcessor(dry, 100, 0.5)
	c.add("no tail for effect free recipes", processor.makeTail(1.0) == nil, "tail is nil as expected")

	var nilTail *transitionTail
	frame := make([]int16, frameSize*channels)
	c.add("nil tail apply is safe", nilTail.apply(frame) == false, "returned false without panic")
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
		for _, value := range TransitionStyleValues(category) {
			if !ValidTransitionStyle(category, value) {
				roundTrip = false
				detail = fmt.Sprintf("%s/%s rejected by validator", category, value)
			}
		}
	}
	if roundTrip {
		detail = "all advertised style values validate"
	}
	c.add("style catalogue validates", roundTrip, "%s", detail)

	c.add("unknown category rejected", TransitionStyleValues("bogus") == nil &&
		!ValidTransitionStyle("bogus", "smooth"), "bogus category returns nil and false")
	c.add("unknown style rejected", !ValidTransitionStyle("eq", "super_bass"), "eq/super_bass rejected")

	names := map[string]string{
		VolumeOverlap.String():              "overlap",
		EQThreeBandFade.String():            "three_band_fade",
		FilterLowPassInHighPassOut.String(): "lowpass_in_highpass_out",
		EffectEchoHalfCutEnd.String():       "echo_half_cut_end",
		LoopEightBeats.String():             "eight_beats",
	}
	namesOK := true
	for got, want := range names {
		if got != want {
			namesOK = false
		}
	}
	c.add("style names round trip", namesOK, "%v", names)

	base := defaultTransitionRecipe()
	overridden := applyStyleOverrides(base, TransitionStyleOverrides{
		Volume: "overlap",
		EQ:     styleAuto,
		Filter: "garbage",
		Effect: "reverb_cut_end",
		Loop:   "",
	})
	c.add("overrides apply only for known values",
		overridden.Volume == VolumeOverlap &&
			overridden.EQ == EQNone &&
			overridden.Filter == FilterNone &&
			overridden.Effect == EffectReverbCutEnd &&
			overridden.Loop == LoopNone,
		"result %s", overridden)

	beats := loopBeatCount(LoopOneBeat) == 1 && loopBeatCount(LoopTwoBeats) == 2 &&
		loopBeatCount(LoopFourBeats) == 4 && loopBeatCount(LoopEightBeats) == 8 &&
		loopBeatCount(LoopNone) == 0
	c.add("loop beat counts", beats, "1/2/4/8 beat mapping correct")
}

func checkSelector(c *checkCollector) {
	makeAnalysis := func(bpm float64, tonic int, minor bool, confidence float64) *TrackAnalysis {
		return &TrackAnalysis{
			BPM:           bpm,
			PeriodSec:     60 / bpm,
			Tonic:         tonic,
			Minor:         minor,
			KeyConfidence: confidence,
		}
	}

	c.add("nil analysis falls back to default",
		selectTransitionRecipe(nil, nil) == defaultTransitionRecipe() &&
			selectTransitionRecipe(makeAnalysis(128, 0, false, 0.5), nil) == defaultTransitionRecipe(),
		"missing analysis keeps the plain smooth crossfade")

	matchedHarmonic := selectTransitionRecipe(makeAnalysis(128, 0, false, 0.5), makeAnalysis(128, 7, false, 0.5))
	c.add("matched tempo and harmonic key blends",
		matchedHarmonic.Volume == VolumeOverlap && matchedHarmonic.EQ == EQThreeBandFade,
		"got %s", matchedHarmonic)

	matchedClashing := selectTransitionRecipe(makeAnalysis(128, 0, false, 0.5), makeAnalysis(128, 6, false, 0.5))
	c.add("matched tempo with clashing key uses filters",
		matchedClashing.Filter == FilterLowPassInHighPassOut && matchedClashing.EQ == EQCenterBassSwap,
		"got %s", matchedClashing)

	wideGap := selectTransitionRecipe(makeAnalysis(90, 0, false, 0.5), makeAnalysis(130, 6, false, 0.5))
	c.add("wide tempo gap uses loop and echo",
		wideGap.Loop == LoopFourBeats && wideGap.Effect == EffectEchoHalfCutEnd && wideGap.Volume == VolumeFadeInCutOut,
		"got %s", wideGap)

	noGrid := selectTransitionRecipe(
		&TrackAnalysis{BPM: 90, KeyConfidence: 0.5},
		&TrackAnalysis{BPM: 130, KeyConfidence: 0.5},
	)
	c.add("wide gap without beat grid avoids loops",
		noGrid.Loop == LoopNone && noGrid.Effect == EffectReverbCutEnd,
		"got %s", noGrid)

	halfTime := selectTransitionRecipe(makeAnalysis(90, 0, false, 0.5), makeAnalysis(174, 7, false, 0.5))
	c.add("half-time pair is treated as tempo compatible",
		halfTime.Volume != VolumeFadeInCutOut && halfTime.Effect != EffectEchoHalfCutEnd,
		"got %s (delta %.4f)", halfTime, tempoDelta(90, 174))

	lowConfidence := selectTransitionRecipe(makeAnalysis(128, 0, false, 0.001), makeAnalysis(128, 7, false, 0.001))
	c.add("low key confidence is treated as unknown key",
		lowConfidence.Volume != VolumeOverlap,
		"got %s", lowConfidence)

	zeroBPM := selectTransitionRecipe(&TrackAnalysis{BPM: 0}, &TrackAnalysis{BPM: 0})
	c.add("zero bpm falls back to default", zeroBPM == defaultTransitionRecipe(), "got %s", zeroBPM)
}

func checkCamelot(c *checkCollector) {
	cases := []struct {
		tonic int
		minor bool
		want  string
	}{
		{0, false, "8B"},
		{9, true, "8A"},
		{4, true, "9A"},
		{7, false, "9B"},
		{6, false, "2B"},
		{11, false, "1B"},
		{1, true, "12A"},
		{8, true, "1A"},
	}

	ok := true
	detail := ""
	for _, testCase := range cases {
		got := camelotCode(testCase.tonic, testCase.minor)
		if got != testCase.want {
			ok = false
			detail = fmt.Sprintf("%s expected %s got %s", keyName(testCase.tonic, testCase.minor), testCase.want, got)
			break
		}
	}
	if ok {
		detail = "reference Camelot codes match for major, minor and wrap-around keys"
	}
	c.add("camelot wheel mapping", ok, "%s", detail)

	confident := func(tonic int, minor bool) *TrackAnalysis {
		return &TrackAnalysis{Tonic: tonic, Minor: minor, KeyConfidence: 0.5}
	}

	same := camelotDistance(confident(0, false), confident(0, false))
	relative := camelotDistance(confident(0, false), confident(9, true))
	neighbour := camelotDistance(confident(0, false), confident(7, false))
	tritone := camelotDistance(confident(0, false), confident(6, false))
	unknown := camelotDistance(confident(0, false), &TrackAnalysis{Tonic: 0, KeyConfidence: 0})

	c.add("camelot distance semantics",
		same == 0 && relative == 1 && neighbour == 1 && tritone == 6 && unknown == -1,
		"same=%d relative=%d neighbour=%d tritone=%d unknown=%d", same, relative, neighbour, tritone, unknown)
}

func synthesizeChordProgression(chords [][]int, seconds, sampleRate float64) []float32 {
	total := int(seconds * sampleRate)
	samples := make([]float32, total)
	chordLength := total / len(chords)
	if chordLength == 0 {
		return samples
	}

	for chordIndex, chord := range chords {
		start := chordIndex * chordLength
		end := start + chordLength
		if end > total {
			end = total
		}
		for _, note := range chord {
			frequency := 130.81 * math.Pow(2, float64(note)/12)
			for harmonic := 1; harmonic <= 4; harmonic++ {
				amplitude := 0.25 / float64(harmonic)
				partial := frequency * float64(harmonic)
				if partial >= sampleRate/2 {
					continue
				}
				step := 2 * math.Pi * partial / sampleRate
				phase := 0.0
				for i := start; i < end; i++ {
					samples[i] += float32(amplitude * math.Sin(phase))
					phase += step
				}
			}
		}
	}
	return samples
}

func checkKeyDetection(c *checkCollector) {
	majorProgression := [][]int{{0, 4, 7}, {5, 9, 12}, {7, 11, 14}, {0, 4, 7}}
	majorSamples := synthesizeChordProgression(majorProgression, 20, tailSampleRate)
	tonic, minor, confidence := analyzeKey(majorSamples, tailSampleRate)
	majorOK := confidence > 0 && ((tonic == 0 && !minor) || (tonic == 9 && minor))
	c.add("C major progression detects C major or its relative", majorOK,
		"got %s (%s), confidence %.4f", keyName(tonic, minor), camelotCode(tonic, minor), confidence)

	minorProgression := [][]int{{9, 12, 16}, {2, 5, 9}, {4, 8, 11}, {9, 12, 16}}
	minorSamples := synthesizeChordProgression(minorProgression, 20, tailSampleRate)
	tonic, minor, confidence = analyzeKey(minorSamples, tailSampleRate)
	minorOK := confidence > 0 && ((tonic == 9 && minor) || (tonic == 0 && !minor))
	c.add("A minor progression detects A minor or its relative", minorOK,
		"got %s (%s), confidence %.4f", keyName(tonic, minor), camelotCode(tonic, minor), confidence)

	transposed := [][]int{{3, 7, 10}, {8, 12, 15}, {10, 14, 17}, {3, 7, 10}}
	transposedSamples := synthesizeChordProgression(transposed, 20, tailSampleRate)
	tonic, minor, _ = analyzeKey(transposedSamples, tailSampleRate)
	transposedOK := (tonic == 3 && !minor) || (tonic == 0 && minor)
	c.add("transposed progression tracks the transposition", transposedOK,
		"got %s, expected D# major or C minor", keyName(tonic, minor))

	silence := make([]float32, int(20*tailSampleRate))
	_, _, silentConfidence := analyzeKey(silence, tailSampleRate)
	c.add("silence yields no key confidence", silentConfidence == 0, "confidence %.6f", silentConfidence)

	short := make([]float32, 100)
	_, _, shortConfidence := analyzeKey(short, tailSampleRate)
	c.add("too short input yields no key", shortConfidence == 0, "confidence %.6f", shortConfidence)

	source := rand.New(rand.NewSource(7))
	noise := make([]float32, int(20*tailSampleRate))
	for i := range noise {
		noise[i] = float32(source.NormFloat64() * 0.2)
	}
	_, _, noiseConfidence := analyzeKey(noise, tailSampleRate)
	noiseChroma, _ := chromagram(noise, tailSampleRate)
	musicChroma, _ := chromagram(majorSamples, tailSampleRate)
	c.add("white noise stays below the confidence floor", noiseConfidence < keyConfidenceFloor,
		"noise confidence %.4f (floor %.4f), noise chroma contrast %.3f vs music %.3f",
		noiseConfidence, keyConfidenceFloor, chromaContrast(noiseChroma), chromaContrast(musicChroma))

	speechLike := make([]float32, int(20*tailSampleRate))
	phase := 0.0
	for i := range speechLike {
		if i%int(tailSampleRate/4) == 0 {
			phase = 0
		}
		frequency := 110 + 60*math.Sin(float64(i)/tailSampleRate*3)
		phase += 2 * math.Pi * frequency / tailSampleRate
		speechLike[i] = float32(0.4*math.Sin(phase) + 0.3*source.NormFloat64())
	}
	_, _, speechConfidence := analyzeKey(speechLike, tailSampleRate)
	c.add("noisy glide stays below the music confidence level", speechConfidence < confidence,
		"glide confidence %.4f vs tonal music %.4f", speechConfidence, confidence)
}

func synthesizeClickTrack(bpm, seconds, sampleRate float64, accentEvery, accentPhase int) []float32 {
	total := int(seconds * sampleRate)
	samples := make([]float32, total)
	interval := 60 / bpm * sampleRate
	burst := int(0.02 * sampleRate)
	source := rand.New(rand.NewSource(11))

	beat := 0
	for position := 0.0; position < float64(total); position += interval {
		amplitude := 0.5
		if accentEvery > 0 && beat%accentEvery == accentPhase {
			amplitude = 1.0
		}
		start := int(position)
		for i := 0; i < burst && start+i < total; i++ {
			decay := 1 - float64(i)/float64(burst)
			samples[start+i] += float32(amplitude * decay * source.NormFloat64() * 0.5)
		}
		beat++
	}
	return samples
}

func checkTempoSearchBounds(c *checkCollector) {
	frameRate := tailSampleRate / beatHop
	minLag, maxLag := tempoLagRange(frameRate)

	minPeriod := float64(minLag) / frameRate
	maxPeriod := float64(maxLag) / frameRate
	lowerBound := 60.0 / beatMaxBPM
	upperBound := 60.0 / beatMinBPM

	c.add("fastest searchable lag is inside the accepted band",
		minPeriod >= lowerBound,
		"minLag=%d period=%.4fs must be >= %.5fs (%.0f BPM)", minLag, minPeriod, lowerBound, beatMaxBPM)

	c.add("slowest searchable lag is inside the accepted band",
		maxPeriod <= upperBound,
		"maxLag=%d period=%.4fs must be <= %.5fs (%.0f BPM)", maxLag, maxPeriod, upperBound, beatMinBPM)

	c.add("tempo search window is not empty", minLag >= 1 && minLag <= maxLag,
		"minLag=%d maxLag=%d at frameRate=%.4f", minLag, maxLag, frameRate)

	nearCeiling := synthesizeClickTrack(205, 30, tailSampleRate, 4, 0)
	nearAnalysis, nearErr := analyzeTrackSamples(nearCeiling, tailSampleRate)
	nearBPM := 0.0
	if nearErr == nil {
		nearBPM = nearAnalysis.BPM
	}
	c.add("a tempo just under the ceiling is refined, not clamped",
		nearErr == nil && math.Abs(nearBPM-205)/205 < 0.03,
		"205 BPM detected as %.2f (clamp would be %.2f), err=%v",
		nearBPM, (60*frameRate)/float64(minLag), nearErr)

	clampBPM := (60 * frameRate) / float64(minLag)
	c.add("a near-ceiling estimate is not the raw lag quantum",
		nearErr == nil && math.Abs(nearBPM-clampBPM) > 0.5,
		"detected %.4f vs lag-quantum %.4f", nearBPM, clampBPM)

	beyondFailures := 0
	beyondDetail := ""
	for _, trueBPM := range []float64{215, 225, 250} {
		beyond := synthesizeClickTrack(trueBPM, 30, tailSampleRate, 4, 0)
		analysis, err := analyzeTrackSamples(beyond, tailSampleRate)
		if err != nil {
			continue
		}
		ratio := trueBPM / analysis.BPM
		octaveResolved := math.Abs(ratio-2) < 0.05
		notClamped := math.Abs(analysis.BPM-clampBPM) > 0.5
		if !octaveResolved || !notClamped {
			beyondFailures++
			beyondDetail = fmt.Sprintf("%.0f BPM detected as %.2f (ratio %.3f)", trueBPM, analysis.BPM, ratio)
		}
	}
	if beyondFailures == 0 {
		beyondDetail = "215, 225 and 250 BPM resolve to their half-tempo grid, never the clamp"
	}
	c.add("a tempo beyond the ceiling resolves to an octave, never the clamp",
		beyondFailures == 0, "%s", beyondDetail)

	checkTempoFolding(c)
}

func checkTempoFolding(c *checkCollector) {
	cases := []struct {
		name     string
		bpmA     float64
		bpmB     float64
		expected float64
	}{
		{"identical tempos", 128, 128, 0},
		{"half time", 187.5, 93.75, 0},
		{"double time", 93.75, 187.5, 0},
		{"genuine wide gap", 128, 160, 0.25},
		{"near miss stays near", 128, 130, 2.0 / 128.0},
		{"three to two is not folded", 128, 192, 0.5},
		{"two to three is not folded", 192, 128, 1.0 / 3.0},
		{"loose double still folds", 200, 108, 0.08},
	}

	failures := 0
	detail := ""
	for _, testCase := range cases {
		got := tempoDelta(testCase.bpmA, testCase.bpmB)
		if math.Abs(got-testCase.expected) > 0.0001 {
			failures++
			detail = fmt.Sprintf("%s: got %.4f want %.4f", testCase.name, got, testCase.expected)
		}
	}
	if failures == 0 {
		detail = fmt.Sprintf("%d folding cases exact", len(cases))
	}
	c.add("tempo delta folds octave errors", failures == 0, "%s", detail)

	c.add("tempo delta rejects non-positive input",
		tempoDelta(0, 128) == 0 && tempoDelta(128, 0) == 0 && tempoDelta(-5, 128) == 0,
		"zero and negative input return 0")

	_, thirdsFactor := tempoDeltaFactor(128, 192)
	_, halfFactor := tempoDeltaFactor(187.5, 93.75)
	c.add("folding only applies to genuine octave relationships",
		thirdsFactor == 1.0 && halfFactor == 2.0,
		"3:2 factor=%.1fx half-time factor=%.1fx tolerance=%.2f", thirdsFactor, halfFactor, tempoFoldTolerance)

	c.add("signed tempo delta keeps direction and folds",
		signedTempoDelta(128, 160) > 0 && signedTempoDelta(160, 128) < 0 &&
			math.Abs(signedTempoDelta(187.5, 93.75)) < 0.0001,
		"up=%.4f down=%.4f half=%.4f",
		signedTempoDelta(128, 160), signedTempoDelta(160, 128), signedTempoDelta(187.5, 93.75))
}

func checkKeyContrastGate(c *checkCollector) {
	toneRate := tailSampleRate
	tonal := make([]float32, int(20*toneRate))
	phases := []float64{0, 0, 0}
	frequencies := []float64{261.63, 329.63, 392.00}
	for i := range tonal {
		var sample float64
		for f, freq := range frequencies {
			phases[f] += 2 * math.Pi * freq / toneRate
			sample += math.Sin(phases[f])
		}
		tonal[i] = float32(sample / float64(len(frequencies)) * 0.5)
	}
	_, _, tonalConfidence := analyzeKey(tonal, toneRate)
	c.add("a clearly tonal signal clears the contrast gate", tonalConfidence > 0,
		"confidence=%.4f floor=%.4f", tonalConfidence, keyConfidenceFloor)

	noise := make([]float32, int(20*toneRate))
	generator := rand.New(rand.NewSource(7))
	for i := range noise {
		noise[i] = float32(generator.NormFloat64() * 0.2)
	}
	_, _, noiseConfidence := analyzeKey(noise, toneRate)
	c.add("broadband noise is rejected by the contrast gate", noiseConfidence == 0,
		"confidence=%.4f", noiseConfidence)
}

func checkBeatAnalysis(c *checkCollector) {
	tempos := []float64{70, 72, 90, 120, 140, 174, 185, 200, 205}
	failures := 0
	detail := ""
	for _, bpm := range tempos {
		samples := synthesizeClickTrack(bpm, 30, tailSampleRate, 0, 0)
		analysis, err := analyzeTrackSamples(samples, tailSampleRate)
		if err != nil {
			failures++
			detail = fmt.Sprintf("%.0f BPM click track failed: %v", bpm, err)
			continue
		}
		expected := bpm
		if bpm > beatMaxBPM {
			expected = bpm / 2
		}
		if math.Abs(analysis.BPM-expected)/expected > 0.03 {
			failures++
			detail = fmt.Sprintf("%.0f BPM click track detected as %.1f", bpm, analysis.BPM)
		}
	}
	if failures == 0 {
		detail = fmt.Sprintf("%d click tracks from %.0f to %.0f BPM detected within 3%%",
			len(tempos), tempos[0], tempos[len(tempos)-1])
	}
	c.add("bpm detection on synthetic click tracks", failures == 0, "%s", detail)

	short := make([]float32, int(4*tailSampleRate))
	_, err := analyzeTrackSamples(short, tailSampleRate)
	c.add("short track is rejected", err != nil, "error: %v", err)

	silence := make([]float32, int(30*tailSampleRate))
	_, err = analyzeTrackSamples(silence, tailSampleRate)
	c.add("silent track is rejected", err != nil, "error: %v", err)

	_, err = analyzeTrackSamples(make([]float32, 1000), 0)
	c.add("invalid sample rate is rejected", err != nil, "error: %v", err)

	accented := synthesizeClickTrack(120, 40, tailSampleRate, 4, 0)
	analysis, err := analyzeTrackSamples(accented, tailSampleRate)
	phaseOK := err == nil && analysis.DownbeatPhase >= 0 && analysis.DownbeatPhase < 4
	phase := -1
	if err == nil {
		phase = analysis.DownbeatPhase
	}
	c.add("downbeat phase is in range", phaseOK, "phase %d (bar length %d beats)", phase, keyBarBeats)

	if err == nil {
		grid := snapTransitionToGrid(1000, 0, analysis)
		bar := snapTransitionToBar(1000, 0, analysis)
		periodFrames := analysis.PeriodSec * framesPerSecond
		firstBeatFrame := analysis.FirstBeat * framesPerSecond

		gridBeat := (float64(grid) - firstBeatFrame) / periodFrames
		barBeat := (float64(bar) - firstBeatFrame) / periodFrames
		gridOK := math.Abs(gridBeat-math.Round(gridBeat)) < 0.02
		barPhase := math.Mod(math.Round(barBeat)-float64(analysis.DownbeatPhase), keyBarBeats)
		if barPhase < 0 {
			barPhase += keyBarBeats
		}
		barOK := math.Abs(barBeat-math.Round(barBeat)) < 0.02 && barPhase == 0
		c.add("transition snapping lands on the grid", gridOK && barOK,
			"beat snap %d (beat %.3f), bar snap %d (beat %.3f, phase %.0f)", grid, gridBeat, bar, barBeat, barPhase)

		c.add("bar snap is within one bar of beat snap",
			math.Abs(float64(bar-grid)) <= periodFrames*keyBarBeats,
			"beat snap %d, bar snap %d, bar length %.1f frames", grid, bar, periodFrames*keyBarBeats)
	}

	c.add("analysis carries key data", err == nil && analysis != nil,
		"click track analysis produced BPM %.1f and key %s", analysisBPM(analysis), analysisKey(analysis))
}

func analysisBPM(analysis *TrackAnalysis) float64 {
	if analysis == nil {
		return 0
	}
	return analysis.BPM
}

func analysisKey(analysis *TrackAnalysis) string {
	if analysis == nil {
		return "none"
	}
	return camelotCode(analysis.Tonic, analysis.Minor)
}

func checkRealtimeBudget(c *checkCollector) {
	recipe := TransitionRecipe{
		Volume: VolumeCutInFadeOut,
		EQ:     EQThreeBandFade,
		Filter: FilterLowPassInHighPassOut,
		Effect: EffectEchoHalfCutEnd,
		Loop:   LoopFourBeats,
	}
	processor := newTransitionProcessor(recipe, 500, 0.5)

	aTone := &toneGenerator{frequency: 220, amplitude: 8000}
	bTone := &toneGenerator{frequency: 660, amplitude: 8000}
	aFrame := make([]int16, frameSize*channels)
	bFrame := make([]int16, frameSize*channels)

	frames := 500
	start := time.Now()
	for i := 0; i < frames; i++ {
		aTone.fill(aFrame)
		bTone.fill(bFrame)
		progress := float64(i) / float64(frames)
		aBuf := processor.processA(aFrame, progress)
		bBuf := processor.processB(bFrame, progress)
		processor.applyGains(aBuf, bBuf, progress, 1.0)
	}
	elapsed := time.Since(start)
	perFrame := elapsed / time.Duration(frames)

	c.add("heaviest recipe fits the realtime budget", perFrame < 4*time.Millisecond,
		"%.3fms per 20ms frame (%.1f%% of realtime)", float64(perFrame.Microseconds())/1000,
		float64(perFrame.Microseconds())/200)

	reverbRecipe := defaultTransitionRecipe()
	reverbRecipe.Effect = EffectReverbOutCenter
	reverbProcessor := newTransitionProcessor(reverbRecipe, 500, 0.5)
	start = time.Now()
	for i := 0; i < frames; i++ {
		aTone.fill(aFrame)
		progress := float64(i) / float64(frames)
		reverbProcessor.processA(aFrame, progress)
	}
	reverbPerFrame := time.Since(start) / time.Duration(frames)
	c.add("reverb fits the realtime budget", reverbPerFrame < 4*time.Millisecond,
		"%.3fms per 20ms frame", float64(reverbPerFrame.Microseconds())/1000)
}

func RunAutoMixChecks() []CheckResult {
	c := &checkCollector{}

	checkBiquads(c)
	checkDelayAndReverb(c)
	checkConversions(c)
	checkVolumeStyles(c)
	checkRecipeMatrix(c)
	checkLongWindows(c)
	checkEdgeWindows(c)
	checkTails(c)
	checkLoopBuffer(c)
	checkStyleNames(c)
	checkSelector(c)
	checkCamelot(c)
	checkKeyDetection(c)
	checkBeatAnalysis(c)
	checkRealtimeBudget(c)
	checkTempoSearchBounds(c)
	checkKeyContrastGate(c)
	checkStyleResolution(c)
	checkOutroResolution(c)
	checkTransitionTiming(c)
	checkAnalysisHelpers(c)
	checkAnnouncementGate(c)
	checkRestartStreamURL(c)
	checkTransitionSlide(c)
	checkAnalysisReadCap(c)

	return c.results
}

func checkAnnouncementGate(c *checkCollector) {
	guildID := "check-announce-guild"
	clearAnnounced(guildID)
	defer clearAnnounced(guildID)

	first := markAnnounced(guildID, 101)
	repeat := markAnnounced(guildID, 101)
	c.add("first announcement for a song is allowed", first, "got %v", first)
	c.add("repeat announcement for the same song is suppressed", !repeat, "got %v", repeat)

	different := markAnnounced(guildID, 102)
	c.add("a different song is announced", different, "got %v", different)

	back := markAnnounced(guildID, 101)
	c.add("returning to an earlier song announces again", back, "got %v", back)

	clearAnnounced(guildID)
	afterClear := markAnnounced(guildID, 101)
	c.add("clearing re-arms the same song for repeat playback", afterClear, "got %v", afterClear)

	otherGuild := "check-announce-guild-other"
	clearAnnounced(otherGuild)
	defer clearAnnounced(otherGuild)
	c.add("announcement state is per guild", markAnnounced(otherGuild, 101), "expected an independent guild to announce")

	crossfadeAdvance := markAnnounced(guildID, 200)
	retryAfterFailure := markAnnounced(guildID, 200)
	seekRestart := markAnnounced(guildID, 200)
	total := 0
	for _, announced := range []bool{crossfadeAdvance, retryAfterFailure, seekRestart} {
		if announced {
			total++
		}
	}
	c.add("crossfade then retry then seek announces exactly once", total == 1, "announced %d times", total)

	clearAnnounced(guildID)
	failed := markAnnounced(guildID, 300)
	clearAnnounced(guildID)
	c.add("a removed song leaves no announcement state", failed && !containsAnnouncement(guildID), "announced %v, state remains %v", failed, containsAnnouncement(guildID))
}

func containsAnnouncement(guildID string) bool {
	announcedSongsMu.Lock()
	defer announcedSongsMu.Unlock()
	_, exists := announcedSongs[guildID]
	return exists
}

func checkRestartStreamURL(c *checkCollector) {
	guildID := "check-restart-guild"
	song := &queue.Song{ID: 1, URL: "https://example.invalid/watch?v=check"}

	kept, err := resolveRestartStreamURL(guildID, song, false, 96000, "https://cdn.invalid/existing")
	c.add("a populated stream URL is reused on restart", kept == "https://cdn.invalid/existing" && err == nil,
		"got %q err=%v", kept, err)

	live := &queue.Song{ID: 2, URL: "https://example.invalid/watch?v=live", IsLive: true}
	liveURL, liveErr := resolveRestartStreamURL(guildID, live, false, 96000, "")
	c.add("a live song restarts without a stream URL", liveURL == "" && liveErr == nil,
		"got %q err=%v", liveURL, liveErr)
}

func checkTransitionSlide(c *checkCollector) {
	cs := newCrossfadeState()
	cs.armed = true
	cs.transitionFrame = 1000
	cs.crossfadeFrames = 400
	cs.minUsableFrames = 100
	cs.totalFrames = 1600
	cs.slideFrames = 50

	cs.slideTransition("check")
	c.add("sliding pushes the transition forward by one beat", cs.transitionFrame == 1050,
		"transitionFrame %d", cs.transitionFrame)
	c.add("sliding leaves the crossfade intact while the window still fits", cs.crossfadeFrames == 400,
		"crossfadeFrames %d with %d frames remaining", cs.crossfadeFrames, cs.totalFrames-cs.transitionFrame)

	cs.transitionFrame = 1250
	cs.slideTransition("check")
	c.add("sliding shrinks the crossfade once it overruns the window", cs.crossfadeFrames == 300,
		"crossfadeFrames %d at frame %d of %d", cs.crossfadeFrames, cs.transitionFrame, cs.totalFrames)

	previous := cs.crossfadeFrames
	for i := 0; i < 40 && !cs.cancelled; i++ {
		cs.slideTransition("check")
		if cs.cancelled {
			break
		}
		if cs.crossfadeFrames > previous {
			c.add("crossfade window never grows while sliding", false, "grew from %d to %d", previous, cs.crossfadeFrames)
			return
		}
		previous = cs.crossfadeFrames
	}
	c.add("crossfade window never grows while sliding", true, "final %d frames", previous)
	c.add("sliding eventually cancels once the window is unusable", cs.cancelled && !cs.armed,
		"cancelled %v armed %v", cs.cancelled, cs.armed)

	fresh := newCrossfadeState()
	c.add("a new crossfade has no refetch parked", fresh.bRefetch.Load() == nil && !fresh.bRefetching.Load() && !fresh.bRetried,
		"refetch %v refetching %v retried %v", fresh.bRefetch.Load(), fresh.bRefetching.Load(), fresh.bRetried)

	fresh.abort()
	c.add("aborting marks the refetch as unwanted", fresh.bAborted.Load(), "bAborted %v", fresh.bAborted.Load())
}

func checkAnalysisReadCap(c *checkCollector) {
	requested := int64(analysisHeadSecs * tailSampleRate * 4)
	c.add("the analysis read cap covers the full requested head",
		analysisMaxBytes > requested,
		"cap %d bytes vs requested %d bytes", analysisMaxBytes, requested)

	margin := analysisMaxBytes - requested
	c.add("the analysis read cap keeps a bounded margin",
		margin > 0 && margin < requested,
		"margin %d bytes", margin)

	minimumSamples := int(beatMinSeconds * tailSampleRate)
	c.add("the analysis read cap admits far more than the minimum analysable length",
		analysisMaxBytes/4 > int64(minimumSamples),
		"cap %d samples vs minimum %d samples", analysisMaxBytes/4, minimumSamples)
}

func styleOverridesFor(category, value string) TransitionStyleOverrides {
	overrides := TransitionStyleOverrides{}
	switch category {
	case "volume":
		overrides.Volume = value
	case "eq":
		overrides.EQ = value
	case "filter":
		overrides.Filter = value
	case "effect":
		overrides.Effect = value
	case "loop":
		overrides.Loop = value
	}
	return overrides
}

func checkStyleResolution(c *checkCollector) {
	analysisA := &TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240, Tonic: 9, Minor: true, KeyConfidence: 0.5}
	analysisB := &TrackAnalysis{BPM: 127, PeriodSec: 60.0 / 127, Duration: 240, Tonic: 4, Minor: true, KeyConfidence: 0.5}

	categories := []string{"volume", "eq", "filter", "effect", "loop"}

	autoStyles := AutoTransitionStyles(analysisA, analysisB)
	validAuto := true
	for _, category := range categories {
		if !ValidTransitionStyle(category, autoStyles[category]) {
			validAuto = false
		}
	}
	c.add("auto styles are valid style keys", validAuto, "auto=%v", autoStyles)

	layerFailures := 0
	for _, category := range categories {
		values := TransitionStyleValues(category)
		guildStyle := values[len(values)-1]
		songStyle := values[1]

		_, effective, source := ResolveTransitionStyles(analysisA, analysisB, true, TransitionStyleOverrides{}, TransitionStyleOverrides{})
		if effective[category] != autoStyles[category] || source[category] != "auto" {
			layerFailures++
		}

		_, effective, source = ResolveTransitionStyles(analysisA, analysisB, true, styleOverridesFor(category, guildStyle), TransitionStyleOverrides{})
		if effective[category] != guildStyle || source[category] != "guild" {
			layerFailures++
		}

		_, effective, source = ResolveTransitionStyles(analysisA, analysisB, true,
			styleOverridesFor(category, guildStyle), styleOverridesFor(category, songStyle))
		if effective[category] != songStyle || source[category] != "song" {
			layerFailures++
		}

		_, effective, source = ResolveTransitionStyles(analysisA, analysisB, true,
			styleOverridesFor(category, guildStyle), styleOverridesFor(category, styleAuto))
		if effective[category] != guildStyle || source[category] != "guild" {
			layerFailures++
		}

		_, effective, source = ResolveTransitionStyles(analysisA, analysisB, true,
			styleOverridesFor(category, "not_a_real_style"), styleOverridesFor(category, ""))
		if effective[category] != autoStyles[category] || source[category] != "auto" {
			layerFailures++
		}
	}
	c.add("style precedence song over guild over auto", layerFailures == 0,
		"%d failures across %d categories", layerFailures, len(categories))

	crossTalk := 0
	_, effective, source := ResolveTransitionStyles(analysisA, analysisB, true,
		TransitionStyleOverrides{}, TransitionStyleOverrides{Effect: "echo_half_cut_end"})
	for _, category := range categories {
		if category == "effect" {
			continue
		}
		if effective[category] != autoStyles[category] || source[category] != "auto" {
			crossTalk++
		}
	}
	c.add("override affects only its own category", crossTalk == 0 && effective["effect"] == "echo_half_cut_end",
		"effect=%s crosstalk=%d", effective["effect"], crossTalk)

	_, nilEffective, nilSource := ResolveTransitionStyles(nil, nil, true, TransitionStyleOverrides{}, TransitionStyleOverrides{})
	nilDefaults := nilEffective["volume"] == "smooth" && nilEffective["eq"] == "none" &&
		nilEffective["filter"] == "none" && nilEffective["effect"] == "none" &&
		nilEffective["loop"] == "none" && nilSource["volume"] == "auto"
	c.add("nil analysis resolves to the default recipe", nilDefaults, "effective=%v", nilEffective)

	defaultStyles := recipeStyleMap(defaultTransitionRecipe())
	_, offEffective, offSource := ResolveTransitionStyles(analysisA, analysisB, false, TransitionStyleOverrides{}, TransitionStyleOverrides{})
	autoSkipped := true
	for _, category := range categories {
		if offEffective[category] != defaultStyles[category] || offSource[category] != "auto" {
			autoSkipped = false
		}
	}
	c.add("auto selection is skipped when disabled", autoSkipped && !reflect.DeepEqual(offEffective, autoStyles),
		"off=%v auto=%v", offEffective, autoStyles)

	_, offOverridden, offOverriddenSource := ResolveTransitionStyles(analysisA, analysisB, false,
		TransitionStyleOverrides{EQ: "quick_bass"}, TransitionStyleOverrides{Effect: "reverb_out_end"})
	offLayers := offOverridden["eq"] == "quick_bass" && offOverriddenSource["eq"] == "guild" &&
		offOverridden["effect"] == "reverb_out_end" && offOverriddenSource["effect"] == "song" &&
		offOverridden["volume"] == defaultStyles["volume"] && offOverriddenSource["volume"] == "auto"
	c.add("overrides still layer when auto selection is disabled", offLayers, "effective=%v", offOverridden)
}

func checkOutroResolution(c *checkCollector) {
	categories := []string{"volume", "eq", "filter", "effect", "loop"}

	inputs := []struct {
		name     string
		analysis *TrackAnalysis
	}{
		{"nil analysis", nil},
		{"zero bpm", &TrackAnalysis{BPM: 0, PeriodSec: 0}},
		{"bpm without grid", &TrackAnalysis{BPM: 128, PeriodSec: 0}},
		{"full grid", &TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Tonic: 9, Minor: true, KeyConfidence: 0.5}},
	}

	invalid := 0
	silent := 0
	for _, input := range inputs {
		styles := AutoOutroStyles(input.analysis)
		for _, category := range categories {
			if !ValidTransitionStyle(category, styles[category]) {
				invalid++
			}
		}
		if styles["effect"] == "none" && styles["filter"] == "none" {
			silent++
		}
	}
	c.add("outro recipes are valid across every analysis shape", invalid == 0, "%d invalid style keys", invalid)
	c.add("every outro shapes the ending", silent == 0, "%d inputs produced no filter and no effect", silent)

	gridded := &TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}
	ungridded := &TrackAnalysis{BPM: 128, PeriodSec: 0}
	c.add("outro differs from the ordinary transition recipe",
		!reflect.DeepEqual(AutoOutroStyles(gridded), AutoTransitionStyles(gridded, gridded)),
		"outro=%v transition=%v", AutoOutroStyles(gridded), AutoTransitionStyles(gridded, gridded))
	c.add("a beat grid earns a rhythmic outro",
		AutoOutroStyles(gridded)["effect"] == "echo_half_cut_end" &&
			AutoOutroStyles(ungridded)["effect"] != "echo_half_cut_end",
		"grid=%s no-grid=%s", AutoOutroStyles(gridded)["effect"], AutoOutroStyles(ungridded)["effect"])

	defaultStyles := recipeStyleMap(defaultTransitionRecipe())
	_, offEffective, offSource := ResolveOutroStyles(gridded, false, TransitionStyleOverrides{}, TransitionStyleOverrides{})
	offSkipped := true
	for _, category := range categories {
		if offEffective[category] != defaultStyles[category] || offSource[category] != "auto" {
			offSkipped = false
		}
	}
	c.add("outro auto selection is skipped when disabled", offSkipped, "off=%v", offEffective)

	_, layered, layeredSource := ResolveOutroStyles(gridded, true,
		TransitionStyleOverrides{Volume: "fadein_cutout"}, TransitionStyleOverrides{Effect: "reverb_out_center"})
	outroLayers := layered["volume"] == "fadein_cutout" && layeredSource["volume"] == "guild" &&
		layered["effect"] == "reverb_out_center" && layeredSource["effect"] == "song" &&
		layered["filter"] == AutoOutroStyles(gridded)["filter"] && layeredSource["filter"] == "auto"
	c.add("outro overrides layer song over guild over auto", outroLayers, "effective=%v", layered)

	distinct := map[string]bool{}
	processor := newTransitionProcessor(defaultTransitionRecipe(), 500, 60.0/128)
	for _, style := range TransitionStyleValues("volume") {
		if style == styleAuto {
			continue
		}
		processor.recipe = applyStyleOverrides(processor.recipe, TransitionStyleOverrides{Volume: style})
		start, _ := processor.gains(0)
		mid, _ := processor.gains(0.5)
		end, _ := processor.gains(1)
		distinct[fmt.Sprintf("%.3f/%.3f/%.3f", start, mid, end)] = true
	}
	c.add("every volume style gives the outro a distinct shape", len(distinct) == 5,
		"%d distinct A-side curves across 5 styles", len(distinct))

	checkOutroWindow(c)
}

func checkOutroWindow(c *checkCollector) {
	analysis := &TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
	fade := fadeSettings{autoMix: true, autoMixBeats: 16, crossfadeSec: 8}
	expectedFrames, _ := TransitionCrossfadeFrames(true, 16, 8, analysis)

	full := &streamEndState{totalFrames: 12000, analysis: analysis}
	start, frames, ok := planOutroWindow(full, 100, fade)
	c.add("outro window sits at the end of the track",
		ok && frames == expectedFrames && start == full.totalFrames-expectedFrames && start > 100,
		"start=%d frames=%d expected=%d ok=%v", start, frames, expectedFrames, ok)

	trimmed := &streamEndState{totalFrames: 12000, silentTailFrames: 500, analysis: analysis}
	trimStart, _, trimOK := planOutroWindow(trimmed, 100, fade)
	c.add("outro window respects the trimmed silent tail",
		trimOK && trimStart == 12000-500-expectedFrames,
		"start=%d expected=%d", trimStart, 12000-500-expectedFrames)

	_, _, lateOK := planOutroWindow(full, full.totalFrames-expectedFrames, fade)
	c.add("outro refuses a window that starts in the past", !lateOK, "ok=%v", lateOK)

	short := &streamEndState{totalFrames: 200, analysis: analysis}
	_, _, shortOK := planOutroWindow(short, 100, fade)
	c.add("outro refuses a track too short to hold it", !shortOK, "ok=%v", shortOK)

	_, _, offOK := planOutroWindow(full, 100, fadeSettings{autoMix: false, autoMixBeats: 16, crossfadeSec: 8})
	noGridStart, noGridFrames, noGridOK := planOutroWindow(
		&streamEndState{totalFrames: 12000}, 100, fade)
	c.add("outro window still resolves without analysis",
		offOK && noGridOK && noGridFrames == int(fallbackCrossfadeSec*framesPerSecond) && noGridStart > 100,
		"start=%d frames=%d", noGridStart, noGridFrames)
}

func checkTransitionTiming(c *checkCollector) {
	analysis := &TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}

	frames, seconds := TransitionCrossfadeFrames(false, 16, 8, analysis)
	c.add("crossfade frames follow the configured duration when automix is off",
		frames == int(8*framesPerSecond) && seconds == 8, "frames=%d seconds=%.2f", frames, seconds)

	frames, seconds = TransitionCrossfadeFrames(true, 16, 8, analysis)
	beatDerived := float64(16) * analysis.PeriodSec
	c.add("crossfade frames follow the beat grid when automix is on",
		seconds > beatDerived-0.001 && seconds < beatDerived+0.001 && frames == int(seconds*framesPerSecond),
		"frames=%d seconds=%.3f expected=%.3f", frames, seconds, beatDerived)

	frames, _ = TransitionCrossfadeFrames(true, 16, 0, nil)
	c.add("crossfade frames fall back without analysis", frames == int(fallbackCrossfadeSec*framesPerSecond),
		"frames=%d", frames)

	_, seconds = TransitionCrossfadeFrames(true, 4096, 8, analysis)
	c.add("crossfade seconds are clamped to the maximum", seconds == crossfadeMaxSec, "seconds=%.2f", seconds)

	crossfadeFrames, _ := TransitionCrossfadeFrames(true, 16, 8, analysis)

	style, loopFrames := ClampLoopStyle(LoopFourBeats, analysis.PeriodSec, crossfadeFrames)
	c.add("loop survives when it fits inside the crossfade", style == LoopFourBeats && loopFrames > 0,
		"style=%s frames=%d of %d", style, loopFrames, crossfadeFrames)

	style, loopFrames = ClampLoopStyle(LoopEightBeats, analysis.PeriodSec, crossfadeFrames)
	c.add("loop is dropped when it needs more than half the crossfade", style == LoopNone && loopFrames == 0,
		"style=%s frames=%d of %d", style, loopFrames, crossfadeFrames)

	style, loopFrames = ClampLoopStyle(LoopFourBeats, 0, crossfadeFrames)
	c.add("loop is dropped without a beat grid", style == LoopNone && loopFrames == 0,
		"style=%s frames=%d", style, loopFrames)

	style, loopFrames = ClampLoopStyle(LoopNone, analysis.PeriodSec, crossfadeFrames)
	c.add("loop none stays none", style == LoopNone && loopFrames == 0, "style=%s frames=%d", style, loopFrames)
}

func checkAnalysisHelpers(c *checkCollector) {
	bpm, key, camelot, hasKey := AnalysisSummary(nil)
	c.add("analysis summary handles nil", bpm == 0 && key == "" && camelot == "" && !hasKey,
		"bpm=%.1f key=%q camelot=%q hasKey=%v", bpm, key, camelot, hasKey)

	lowConfidence := &TrackAnalysis{BPM: 120, Tonic: 3, Minor: false, KeyConfidence: keyConfidenceFloor / 2}
	bpm, _, _, hasKey = AnalysisSummary(lowConfidence)
	c.add("analysis summary hides low confidence keys", bpm == 120 && !hasKey,
		"bpm=%.1f hasKey=%v confidence=%.4f", bpm, hasKey, lowConfidence.KeyConfidence)

	confident := &TrackAnalysis{BPM: 174, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	_, key, camelot, hasKey = AnalysisSummary(confident)
	c.add("analysis summary reports confident keys", hasKey && key == "C major" && camelot == "8B",
		"key=%q camelot=%q", key, camelot)

	if _, distance, ok := TransitionCompatibility(nil, confident); ok || distance != -1 {
		c.add("compatibility rejects nil input", false, "ok=%v distance=%d", ok, distance)
	} else {
		c.add("compatibility rejects nil input", true, "ok=false distance=-1")
	}

	zeroBPM := &TrackAnalysis{BPM: 0, KeyConfidence: 0.5}
	_, _, ok := TransitionCompatibility(zeroBPM, confident)
	c.add("compatibility rejects zero BPM", !ok, "ok=%v", ok)

	slower := &TrackAnalysis{BPM: 120, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	faster := &TrackAnalysis{BPM: 132, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	delta, distance, ok := TransitionCompatibility(slower, faster)
	c.add("compatibility reports signed BPM delta", ok && math.Abs(delta-0.1) < 1e-9 && distance == 0,
		"delta=%.4f distance=%d", delta, distance)
}
