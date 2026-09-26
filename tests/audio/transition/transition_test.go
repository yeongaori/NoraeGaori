package transition_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
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

func TestOutroGainStartsAtThePlayingLevelAndEndsSilent(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Volume = transition.VolumeOverlap
	processor := transition.NewProcessor(recipe, 200, 0.5)
	buf := make([]float64, dsp.FrameSize*dsp.Channels)

	var first, last float64
	for frame := 0; frame < 200; frame++ {
		for i := range buf {
			buf[i] = 10000
		}
		processor.ApplyGainA(buf, float64(frame)/200, 0.5)
		if frame == 0 {
			first = buf[0]
		}
		last = buf[len(buf)-1]
	}

	if math.Abs(first-5000) > 1 {
		t.Errorf("first outro sample = %.1f, want 5000 (the 50%% volume already playing)", first)
	}
	if last > 500 {
		t.Errorf("last outro sample = %.1f, want under 500 (10%% of the starting level) as it reaches silence", last)
	}
}

func sideLossDB(filter transition.FilterStyle, frequency, progress float64, isOutgoing bool) float64 {
	recipe := transition.DefaultRecipe()
	recipe.Filter = filter
	return recipeLossDB(recipe, frequency, progress, isOutgoing)
}

func recipeLossDB(recipe transition.Recipe, frequency, progress float64, isOutgoing bool) float64 {
	processor := transition.NewProcessor(recipe, 1000000, 0.5)
	tone := &audiotest.ToneGenerator{Frequency: frequency, Amplitude: 10000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	input := make([]float64, len(frame))

	var output []float64
	for i := 0; i < 20; i++ {
		tone.Fill(frame)
		if isOutgoing {
			output = processor.ProcessA(frame, progress)
		} else {
			output = processor.ProcessB(frame, progress)
		}
	}
	dsp.FrameToFloat(frame, input)
	return -20 * math.Log10(audiotest.BufferRMS(output)/audiotest.BufferRMS(input))
}

func TestLowPassOpensEarlyOnTheWayInAndClosesLateOnTheWayOut(t *testing.T) {
	if loss := sideLossDB(transition.FilterLowPassIn, 8000, 0.5, false); loss > 6 {
		t.Errorf("incoming 8 kHz lost %.2f dB halfway, want at most 6 dB", loss)
	}
	if loss := sideLossDB(transition.FilterLowPassOut, 8000, 0.5, true); loss > 6 {
		t.Errorf("outgoing 8 kHz lost %.2f dB halfway, want at most 6 dB", loss)
	}
	if loss := sideLossDB(transition.FilterLowPassIn, 8000, 0.1, false); loss < 20 {
		t.Errorf("incoming 8 kHz lost %.2f dB at 10%%, want at least 20 dB while it is still closed", loss)
	}
}

func TestCutInRampsTheIncomingSongUpOverAQuarterBeat(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Volume = transition.VolumeCutInFadeOut
	processor := transition.NewProcessor(recipe, 200, 0.5)
	aBuf := make([]float64, dsp.FrameSize*dsp.Channels)
	bBuf := make([]float64, dsp.FrameSize*dsp.Channels)

	ends := []float64{}
	for frame := 0; frame < 10; frame++ {
		for i := range bBuf {
			bBuf[i] = 10000
		}
		processor.ApplyGains(aBuf, bBuf, float64(frame)/200, 1.0)
		if frame == 0 && bBuf[0] > 100 {
			t.Errorf("first incoming sample = %.0f, want under 100 so the cut-in starts from silence", bBuf[0])
		}
		ends = append(ends, bBuf[len(bBuf)-1])
	}

	if ends[0] > 1000 {
		t.Errorf("incoming level after one frame = %.0f, want under 1000: the cut takes a quarter beat, not one frame", ends[0])
	}
	if ends[9] < 8000 {
		t.Errorf("incoming level after ten frames = %.0f, want at least 8000 once the quarter-beat cut is done", ends[9])
	}
}

func TestCutOutHoldsTheOutgoingSongUntilTheFinalQuarterBeat(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Volume = transition.VolumeFadeInCutOut
	processor := transition.NewProcessor(recipe, 200, 0.5)

	if held, _ := processor.Gains(0.95); held < 0.9 {
		t.Errorf("outgoing gain at 95%% = %.3f, want at least 0.9 before the cut", held)
	}
	if cut, _ := processor.Gains(1); cut != 0 {
		t.Errorf("outgoing gain at the handoff = %.3f, want 0 so the cut does not click", cut)
	}
}

func TestThreeBandFadeCutsHighsWithoutKillingThem(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.EQ = transition.EQThreeBandFade

	if loss := recipeLossDB(recipe, 8000, 0.75, true); loss < 10 || loss > 16 {
		t.Errorf("outgoing 8 kHz lost %.2f dB after the highs swap, want a 10-16 dB cut rather than a -40 dB kill", loss)
	}
	if loss := recipeLossDB(recipe, 8000, 0.25, false); loss < 4 || loss > 12 {
		t.Errorf("incoming 8 kHz lost %.2f dB halfway through the highs swap, want 4-12 dB (half of the -15 dB cut)", loss)
	}
}

func TestHighPassKeepsTheOutgoingBodyUntilLate(t *testing.T) {
	if loss := sideLossDB(transition.FilterLowPassInHighPassOut, 400, 0.75, true); loss > 3 {
		t.Errorf("outgoing 400 Hz lost %.2f dB at 75%% of the mix, want at most 3 dB", loss)
	}
	if loss := sideLossDB(transition.FilterLowPassInHighPassOut, 400, 1, true); loss < 12 {
		t.Errorf("outgoing 400 Hz lost %.2f dB at the end, want at least 12 dB once the high-pass has risen", loss)
	}
}

func TestLowPassHasNoResonantBump(t *testing.T) {
	for frequency := 300.0; frequency <= 3000; frequency += 100 {
		if loss := sideLossDB(transition.FilterLowPassOut, frequency, 0.75, true); loss < -0.1 {
			t.Errorf("%.0f Hz gained %.2f dB below the 2.2 kHz cutoff, want a flat passband with no resonant bump", frequency, -loss)
		}
	}
}

func TestIncomingSideNeverGetsTheEffect(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Effect = transition.EffectReverbCutEnd
	processor := transition.NewProcessor(recipe, 100, 0.5)
	tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	for i := 0; i < 100; i++ {
		tone.Fill(frame)
		buf := processor.ProcessB(frame, float64(i)/100)
		for j, sample := range buf {
			if sample != float64(frame[j]) {
				t.Fatalf("frame %d sample %d = %.1f, want the dry %d: the effect belongs to the outgoing song only", i, j, sample, frame[j])
			}
		}
	}
}

func TestFilterSweepMovesWithinEachFrame(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Filter = transition.FilterLowPassOut
	processor := transition.NewProcessor(recipe, 50, 0.5)
	tone := &audiotest.ToneGenerator{Frequency: 3000, Amplitude: 10000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	window := 64 * dsp.Channels

	for i := 0; i < 45; i++ {
		tone.Fill(frame)
		processor.ProcessA(frame, float64(i)/50)
	}
	tone.Fill(frame)
	buf := processor.ProcessA(frame, 45.0/50)

	early := audiotest.BufferRMS(buf[window : 2*window])
	late := audiotest.BufferRMS(buf[len(buf)-window:])
	if late >= early*0.97 {
		t.Errorf("late-frame level %.0f vs early %.0f, want the closing filter to lower it within the frame", late, early)
	}
}

func runTransitionWindow(recipe transition.Recipe, crossfadeFrames int, periodSec float64) (bool, float64, float64, int) {
	processor := transition.NewProcessor(recipe, crossfadeFrames, periodSec)
	aTone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 8000}
	bTone := &audiotest.ToneGenerator{Frequency: 660, Amplitude: 8000}

	aFrame := make([]int16, dsp.FrameSize*dsp.Channels)
	bFrame := make([]int16, dsp.FrameSize*dsp.Channels)
	mixed := make([]int16, dsp.FrameSize*dsp.Channels)

	finite := true
	maxJump := 0.0
	maxPeak := 0.0
	clipped := 0
	previousSample := 0.0

	for frame := 0; frame < crossfadeFrames; frame++ {
		aTone.Fill(aFrame)
		bTone.Fill(bFrame)
		progress := float64(frame) / float64(crossfadeFrames)

		aBuf := processor.ProcessA(aFrame, progress)
		bBuf := processor.ProcessB(bFrame, progress)
		if !audiotest.IsBufferFinite(aBuf) || !audiotest.IsBufferFinite(bBuf) {
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

			if i%dsp.Channels == 0 {
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

	loud := make([]int16, dsp.FrameSize*dsp.Channels)
	for i := range loud {
		if i%2 == 0 {
			loud[i] = 32767
		} else {
			loud[i] = -32768
		}
	}

	if aBuf := processor.ProcessA(loud, 0.5); !audiotest.IsBufferFinite(aBuf) {
		t.Errorf("outgoing buffer is not finite (peak %.0f)", audiotest.BufferPeak(aBuf))
	}
	if bBuf := processor.ProcessB(loud, 0.5); !audiotest.IsBufferFinite(bBuf) {
		t.Errorf("incoming buffer is not finite (peak %.0f)", audiotest.BufferPeak(bBuf))
	}
}

func TestNilFrameIsTreatedAsSilence(t *testing.T) {
	processor := transition.NewProcessor(transition.DefaultRecipe(), 100, 0.5)

	if peak := audiotest.BufferPeak(processor.ProcessA(nil, 0.5)); peak != 0 {
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

			tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
			frame := make([]int16, dsp.FrameSize*dsp.Channels)
			for i := 0; i < 100; i++ {
				tone.Fill(frame)
				progress := float64(i) / 100
				aBuf := processor.ProcessA(frame, progress)
				bBuf := processor.ProcessB(frame, progress)
				processor.ApplyGains(aBuf, bBuf, progress, 1.0)
			}

			tail := processor.MakeHandoffTail(1.0)
			if tail == nil {
				t.Fatal("produced no tail")
			}

			budget := max(transition.HandoffEchoTailFrames, transition.HandoffReverbTailFrames)
			silent := make([]int16, dsp.FrameSize*dsp.Channels)
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

func countTailFrames(tail *transition.Tail) int {
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	frames := 1
	for tail.Apply(frame) {
		frames++
	}
	return frames
}

func TestOutroTailRingsOutLongerThanTheHandoffTail(t *testing.T) {
	cases := []struct {
		effect        transition.EffectStyle
		outroFrames   int
		handoffFrames int
	}{
		{transition.EffectReverbOutEnd, transition.ReverbTailFrames, transition.HandoffReverbTailFrames},
		{transition.EffectEchoHalfCutEnd, transition.EchoTailFrames, transition.HandoffEchoTailFrames},
	}

	for _, testCase := range cases {
		t.Run(testCase.effect.String(), func(t *testing.T) {
			recipe := transition.DefaultRecipe()
			recipe.Effect = testCase.effect
			processor := transition.NewProcessor(recipe, 100, 0.5)
			frame := make([]int16, dsp.FrameSize*dsp.Channels)
			for i := 0; i < 100; i++ {
				processor.ProcessA(frame, float64(i)/100)
			}

			if got := countTailFrames(processor.MakeTail(1.0)); got != testCase.outroFrames {
				t.Errorf("outro tail lasted %d frames, want %d", got, testCase.outroFrames)
			}
			if got := countTailFrames(processor.MakeHandoffTail(1.0)); got != testCase.handoffFrames {
				t.Errorf("handoff tail lasted %d frames, want %d", got, testCase.handoffFrames)
			}
		})
	}
}

func TestHandoffAndOutroTailsStartAtTheSameLevel(t *testing.T) {
	firstPeak := func(makeTail func(*transition.Processor) *transition.Tail) float64 {
		recipe := transition.DefaultRecipe()
		recipe.Effect = transition.EffectReverbCutEnd
		processor := transition.NewProcessor(recipe, 100, 0.5)
		tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
		frame := make([]int16, dsp.FrameSize*dsp.Channels)
		for i := 0; i < 100; i++ {
			tone.Fill(frame)
			processor.ProcessA(frame, float64(i)/100)
		}
		silent := make([]int16, dsp.FrameSize*dsp.Channels)
		makeTail(processor).Apply(silent)
		peak := 0.0
		for _, sample := range silent {
			peak = math.Max(peak, math.Abs(float64(sample)))
		}
		return peak
	}

	outro := firstPeak(func(p *transition.Processor) *transition.Tail { return p.MakeTail(1.0) })
	handoff := firstPeak(func(p *transition.Processor) *transition.Tail { return p.MakeHandoffTail(1.0) })
	if outro == 0 || math.Abs(handoff-outro) > 1 {
		t.Errorf("first handoff tail frame peaked at %.0f, outro at %.0f, want the same starting level", handoff, outro)
	}
}

func echoAfterMix(amplitude float64) *transition.Processor {
	recipe := transition.DefaultRecipe()
	recipe.Effect = transition.EffectEchoHalfCutEnd
	processor := transition.NewProcessor(recipe, 100, 0.5)
	tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: amplitude}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for i := 0; i < 100; i++ {
		tone.Fill(frame)
		processor.ProcessA(frame, float64(i)/100)
	}
	return processor
}

func echoTailPeaks(gain float64, isHandoff bool, frames int) []float64 {
	processor := echoAfterMix(5000)
	tail := processor.MakeTail(gain)
	if isHandoff {
		tail = processor.MakeHandoffTail(gain)
	}
	peaks := make([]float64, 0, frames)
	for i := 0; i < frames; i++ {
		silent := make([]int16, dsp.FrameSize*dsp.Channels)
		tail.Apply(silent)
		peak := 0.0
		for _, sample := range silent {
			peak = math.Max(peak, math.Abs(float64(sample)))
		}
		peaks = append(peaks, peak)
	}
	return peaks
}

func applyTailOverLoudSong(t *testing.T) (frames int, longestRail int, lastDeviation float64) {
	t.Helper()
	tail := echoAfterMix(20000).MakeHandoffTail(1.0)
	song := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 32000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	input := make([]int16, len(frame))

	run := 0
	for more := true; more; frames++ {
		if frames > 200 {
			t.Fatal("the tail never let go of the next song")
		}
		song.Fill(frame)
		copy(input, frame)
		more = tail.Apply(frame)

		lastDeviation = 0
		for i := 0; i < len(frame); i += dsp.Channels {
			if frame[i] == 32767 || frame[i] <= -32767 {
				run++
				longestRail = max(longestRail, run)
			} else {
				run = 0
			}
			lastDeviation = math.Max(lastDeviation, math.Abs(float64(frame[i])-float64(input[i])))
		}
	}
	return frames, longestRail, lastDeviation
}

func TestHandoffTailIsLimitedOverTheNextSong(t *testing.T) {
	_, longestRail, _ := applyTailOverLoudSong(t)

	if longestRail > 2 {
		t.Errorf("the next song plus the echo held the rail for %d samples in a row, want a limited peak instead of clipping", longestRail)
	}
}

func TestQuietTailLeavesTheNextSongUnlimited(t *testing.T) {
	tail := echoAfterMix(5000).MakeHandoffTail(0.5)
	song := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 20000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	song.Fill(frame)

	tail.Apply(frame)

	peak := 0.0
	for _, sample := range frame {
		peak = math.Max(peak, math.Abs(float64(sample)))
	}
	if peak < 20000 {
		t.Errorf("song plus a half-level tail peaked at %.0f, want at least the song's own 20000: nothing here reaches full scale", peak)
	}
}

func TestFinishedTailLeavesFramesAlone(t *testing.T) {
	tail := echoAfterMix(5000).MakeHandoffTail(1.0)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for tail.Apply(frame) {
		for i := range frame {
			frame[i] = 0
		}
	}

	song := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 20000}
	song.Fill(frame)
	want := append([]int16(nil), frame...)
	if tail.Apply(frame) {
		t.Error("a finished tail reported more audio to come")
	}
	for i := range frame {
		if frame[i] != want[i] {
			t.Fatalf("sample %d = %d after the tail finished, want the untouched %d", i, frame[i], want[i])
		}
	}
}

func TestTailHandsTheNextSongBackUntouched(t *testing.T) {
	frames, _, lastDeviation := applyTailOverLoudSong(t)

	if frames <= transition.HandoffEchoTailFrames {
		t.Errorf("the tail stopped after %d frames, want it to keep limiting past the %d-frame ring-out until the limiter lets go", frames, transition.HandoffEchoTailFrames)
	}
	if lastDeviation > 32 {
		t.Errorf("the last frame the tail touched differs from the song by %.0f, want at most 32 (0.1%%) so there is no level step when it ends", lastDeviation)
	}
}

func TestCutEndEffectsLeaveTheirTailAudible(t *testing.T) {
	for _, effect := range []transition.EffectStyle{transition.EffectReverbCutEnd, transition.EffectEchoHalfCutEnd} {
		t.Run(effect.String(), func(t *testing.T) {
			recipe := transition.DefaultRecipe()
			recipe.Volume = transition.VolumeFadeInCutOut
			recipe.Effect = effect
			processor := transition.NewProcessor(recipe, 100, 0.5)
			tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
			frame := make([]int16, dsp.FrameSize*dsp.Channels)
			silent := make([]int16, dsp.FrameSize*dsp.Channels)
			for i := 0; i < 100; i++ {
				tone.Fill(frame)
				progress := float64(i) / 100
				aBuf := processor.ProcessA(frame, progress)
				bBuf := processor.ProcessB(silent, progress)
				processor.ApplyGains(aBuf, bBuf, progress, 1.0)
			}

			if gain := processor.LastGain(); gain < 0.95 {
				t.Errorf("outgoing gain at the handoff = %.3f, want at least 0.95: the effect cuts the dry signal itself, so its tail must keep ringing", gain)
			}
			tailFrame := make([]int16, dsp.FrameSize*dsp.Channels)
			processor.MakeHandoffTail(processor.LastGain()).Apply(tailFrame)
			peak := 0.0
			for _, sample := range tailFrame {
				peak = math.Max(peak, math.Abs(float64(sample)))
			}
			if peak < 500 {
				t.Errorf("first handoff tail frame peaked at %.0f, want at least 500 so the effect is heard over the next song", peak)
			}
		})
	}
}

func TestHandoffTailFadesAlongAQuarterSine(t *testing.T) {
	handoff := echoTailPeaks(1.0, true, 16)
	outro := echoTailPeaks(1.0, false, 16)

	if outro[15] == 0 {
		t.Fatal("the outro tail was silent at frame 15, want the echo still ringing")
	}
	ratio := handoff[15] / outro[15]
	if ratio < 0.87 || ratio > 0.93 {
		t.Errorf("handoff/outro level at frame 15 = %.3f, want about 0.900 (cos(pi/2*15/50) / cos(pi/2*15/170))", ratio)
	}
}

func TestTailFollowsTheGainItWasGiven(t *testing.T) {
	full := echoTailPeaks(1.0, true, 1)[0]
	half := echoTailPeaks(0.5, true, 1)[0]

	if full == 0 {
		t.Fatal("the tail was silent, want the echo to continue")
	}
	if math.Abs(half-full/2) > 1 {
		t.Errorf("tail peak at gain 0.5 = %.0f, want half of %.0f at gain 1", half, full)
	}
}

func TestTailStartsAtItsGainAndFadesSmoothly(t *testing.T) {
	recipe := transition.DefaultRecipe()
	recipe.Effect = transition.EffectReverbCutEnd
	processor := transition.NewProcessor(recipe, 100, 0.5)
	tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for i := 0; i < 100; i++ {
		tone.Fill(frame)
		processor.ProcessA(frame, float64(i)/100)
	}

	tail := processor.MakeHandoffTail(1.0)
	levels := []float64{}
	for {
		silent := make([]int16, dsp.FrameSize*dsp.Channels)
		more := tail.Apply(silent)
		peak := 0.0
		for _, sample := range silent {
			peak = math.Max(peak, math.Abs(float64(sample)))
		}
		levels = append(levels, peak)
		if !more {
			break
		}
	}

	if levels[0] == 0 {
		t.Fatal("the tail was silent from its first frame, want it to continue the reverb")
	}
	if last := levels[len(levels)-1]; last > levels[0]*0.05 {
		t.Errorf("last tail frame peak %.0f, want under 5%% of the first (%.0f)", last, levels[0])
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

	if tail.Apply(make([]int16, dsp.FrameSize*dsp.Channels)) {
		t.Error("a nil tail reported more audio to come")
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
