package transition_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

type styleChoice struct {
	category transition.Category
	style    string
}

func volumeStyle(style string) styleChoice {
	return styleChoice{transition.ShortcutVolume, style}
}

func eqStyle(style string) styleChoice {
	return styleChoice{transition.ShortcutEQ, style}
}

func filterStyle(style string) styleChoice {
	return styleChoice{transition.ShortcutFilter, style}
}

func effectStyle(style string) styleChoice {
	return styleChoice{transition.ShortcutEffect, style}
}

func sideStyle(category transition.Category, style string) styleChoice {
	return styleChoice{category, style}
}

func legacyNames(category transition.Category) []string {
	return transition.StyleValues(category)[1:]
}

func combinedRecipe(choices ...styleChoice) *transition.Recipe {
	recipe := transition.DefaultRecipe()
	for _, choice := range choices {
		recipe.Apply(transition.ExpandLegacy(choice.category, choice.style))
	}
	return &recipe
}

func eightBarWindow(frames int) *transition.Window {
	return &transition.Window{Frames: frames, PeriodSec: 0.5, Bars: 8}
}

func volumeOnly(style string) *transition.Processor {
	return transition.NewProcessor(combinedRecipe(volumeStyle(style)), eightBarWindow(800))
}

func TestVolumeStylesStartOnTheOutgoingSongAndEndOnTheIncoming(t *testing.T) {
	for _, style := range legacyNames(transition.ShortcutVolume) {
		processor := volumeOnly(style)
		startOut, startIn := processor.Gains(0)
		endOut, endIn := processor.Gains(1)
		if startOut != 1 || startIn != 0 || endOut != 0 || endIn != 1 {
			t.Errorf("%s runs (%.2f, %.2f) to (%.2f, %.2f), want (1, 0) to (0, 1)", style, startOut, startIn, endOut, endIn)
		}
		previousOut, previousIn := startOut, startIn
		for step := 1; step <= 1000; step++ {
			out, in := processor.Gains(float64(step) / 1000)
			if out > previousOut+1e-9 || in < previousIn-1e-9 {
				t.Errorf("%s at %.3f: outgoing %.4f after %.4f, incoming %.4f after %.4f, want a one-way fade", style, float64(step)/1000, out, previousOut, in, previousIn)
				break
			}
			previousOut, previousIn = out, in
		}
	}
}

func TestVolumeStylesFollowTheirCurves(t *testing.T) {
	quarter := 1.0 / (8 * 16)
	tick := 1.0 / (8 * 256)
	cases := []struct {
		style    string
		progress float64
		out, in  float64
	}{
		{"crossfade", 0.25, 0.75, 0.25},
		{"fadein_fadeout", 0.25, 1, 0.5},
		{"fadein_fadeout", 0.75, 0.5, 1},
		{"overlap", tick, 1, 1},
		{"overlap", 1 - tick, 1, 1},
		{"cut", 0.5 - quarter, 1, 0},
		{"cut", 0.5 - quarter/2, 1, 0.5},
		{"cut", 0.5 + quarter/2, 0.5, 1},
		{"cut", 0.5 + quarter, 0, 1},
		{"fadein_cutout", 0.75, 1, 1},
		{"cutin_fadeout", 0.75, 0.5, 1},
		{"fadein_fastout", 0.875, 1, 1},
		{"fadein_fastout", 0.9375, 0.5, 1},
		{"smooth", 0.5, 0.6411, 0.6411},
		{"smooth", 0.25, 0.9289, 0.1699},
	}
	for _, c := range cases {
		out, in := volumeOnly(c.style).Gains(c.progress)
		if math.Abs(out-c.out) > 1e-3 || math.Abs(in-c.in) > 1e-3 {
			t.Errorf("%s at %.5f = (%.4f, %.4f), want (%.4f, %.4f)", c.style, c.progress, out, in, c.out, c.in)
		}
	}
}

func TestShortOverlapsShapeCurvesAsTwoBars(t *testing.T) {
	fallback := transition.NewProcessor(combinedRecipe(volumeStyle("cut")), &transition.Window{Frames: 250})
	twoBars := transition.NewProcessor(combinedRecipe(volumeStyle("cut")), &transition.Window{Frames: 250, Bars: 2})

	for _, progress := range []float64{0.47, 0.49, 0.5, 0.51, 0.53} {
		a, b := fallback.Gains(progress)
		c, d := twoBars.Gains(progress)
		if a != c || b != d {
			t.Errorf("at %.2f the unbarred overlap gives (%.3f, %.3f), want the two-bar (%.3f, %.3f)", progress, a, b, c, d)
		}
	}
}

func steadyLevel(processor *transition.Processor, outgoing bool, frequency, progress float64) float64 {
	tone := &audiotest.ToneGenerator{Frequency: frequency, Amplitude: 10000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	var output []float64
	for i := 0; i < 25; i++ {
		tone.Fill(frame)
		if outgoing {
			output = processor.Mix(frame, silent, progress, 1)
		} else {
			output = processor.Mix(silent, frame, progress, 1)
		}
	}
	return audiotest.Decibels(audiotest.BufferRMS(output) / (10000 / math.Sqrt2))
}

func shaped(choices ...styleChoice) *transition.Processor {
	return transition.NewProcessor(combinedRecipe(append([]styleChoice{volumeStyle("overlap")}, choices...)...), eightBarWindow(800))
}

func TestBassSwapTradesTheLowsAtTheMiddle(t *testing.T) {
	cases := []struct {
		outgoing bool
		progress float64
		killed   bool
	}{
		{true, 0.4, false}, {true, 0.6, true}, {false, 0.4, true}, {false, 0.6, false},
	}
	for _, c := range cases {
		level := steadyLevel(shaped(eqStyle("center_bass_swap")), c.outgoing, 50, c.progress)
		if c.killed && level > -30 {
			t.Errorf("outgoing=%t at %.1f keeps 50Hz at %+.1f dB, want it killed", c.outgoing, c.progress, level)
		}
		if !c.killed && math.Abs(level) > 0.5 {
			t.Errorf("outgoing=%t at %.1f plays 50Hz at %+.1f dB, want it untouched", c.outgoing, c.progress, level)
		}
		if mid := steadyLevel(shaped(eqStyle("center_bass_swap")), c.outgoing, 1000, c.progress); math.Abs(mid) > 0.5 {
			t.Errorf("outgoing=%t at %.1f moves 1kHz by %+.1f dB, want only the bass swapped", c.outgoing, c.progress, mid)
		}
	}
}

func TestThreeBandFadeDipsTheHighsByTheMeasuredCut(t *testing.T) {
	level := steadyLevel(shaped(eqStyle("three_band_fade")), true, 12000, 0.75)
	if math.Abs(level+11.7) > 1 {
		t.Errorf("outgoing 12kHz after the highs swap = %+.1f dB, want about -11.7 dB", level)
	}
	if mid := steadyLevel(shaped(eqStyle("three_band_fade")), false, 1000, 0.1); mid > -8 {
		t.Errorf("incoming 1kHz early in the mix = %+.1f dB, want its mids held down", mid)
	}
}

func TestLowPassOutClosesOverTheSecondHalf(t *testing.T) {
	processor := func() *transition.Processor { return shaped(filterStyle("lowpass_out")) }

	if level := steadyLevel(processor(), true, 6000, 0.4); math.Abs(level) > 0.5 {
		t.Errorf("outgoing 6kHz before the sweep = %+.1f dB, want untouched", level)
	}
	if level := steadyLevel(processor(), true, 6000, 1); level > -30 {
		t.Errorf("outgoing 6kHz at the end = %+.1f dB, want closed down to the 120Hz cutoff", level)
	}
	if level := steadyLevel(processor(), false, 6000, 0.1); math.Abs(level) > 0.5 {
		t.Errorf("incoming 6kHz = %+.1f dB, want the incoming side unfiltered", level)
	}
}

func TestHighPassInOpensOverTheFirstHalf(t *testing.T) {
	processor := func() *transition.Processor { return shaped(filterStyle("highpass_in")) }

	if level := steadyLevel(processor(), false, 300, 0.01); level > -20 {
		t.Errorf("incoming 300Hz at the start = %+.1f dB, want cut by the 9kHz high-pass", level)
	}
	if level := steadyLevel(processor(), false, 300, 0.6); math.Abs(level) > 0.5 {
		t.Errorf("incoming 300Hz after the sweep = %+.1f dB, want untouched", level)
	}
}

func TestFullOverlapIsAPlainSum(t *testing.T) {
	processor := volumeOnly("overlap")
	aTone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
	bTone := &audiotest.ToneGenerator{Frequency: 330, Amplitude: 9000}
	aFrame := make([]int16, dsp.FrameSize*dsp.Channels)
	bFrame := make([]int16, dsp.FrameSize*dsp.Channels)
	aTone.Fill(aFrame)
	bTone.Fill(bFrame)

	mixed := processor.Mix(aFrame, bFrame, 0.5, 0.8)
	for i := range mixed {
		want := 0.8 * (float64(aFrame[i]) + float64(bFrame[i]))
		if math.Abs(mixed[i]-want) > 1e-6 {
			t.Fatalf("sample %d = %.3f, want the plain sum %.3f with no ducking", i, mixed[i], want)
		}
	}
}

func dryThroughout(t *testing.T, recipe *transition.Recipe, outgoing bool) {
	t.Helper()
	processor := transition.NewProcessor(recipe, eightBarWindow(100))
	tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	for i := 1; i < 99; i++ {
		tone.Fill(frame)
		a, b := silent, frame
		if outgoing {
			a, b = frame, silent
		}
		mixed := processor.Mix(a, b, float64(i)/100, 1)
		for j, sample := range mixed {
			if math.Abs(sample-float64(frame[j])) > 1e-6 {
				t.Fatalf("%s frame %d sample %d = %.1f, want the dry %d", recipe, i, j, sample, frame[j])
			}
		}
	}
}

func TestIncomingSideNeverGetsTheOutgoingEffect(t *testing.T) {
	dryThroughout(t, combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXOut, "reverb_out_center")), false)
}

func TestOutgoingSideNeverGetsTheIncomingEffect(t *testing.T) {
	for _, fx := range []string{"phaser", "bitcrusher", "delay_one_bar", "delay_one_bar_at_end"} {
		dryThroughout(t, combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXIn, fx)), true)
	}
}

func TestSendIsFedBeforeItIsHeard(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), effectStyle("echo_beat_out_end")), eightBarWindow(200))
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	silent := make([]int16, dsp.FrameSize*dsp.Channels)

	var late float64
	for i := 0; i < 200; i++ {
		input := silent
		if i < 75 {
			for j := range frame {
				frame[j] = int16(8000 * math.Sin(float64(j/2)*0.03))
			}
			input = frame
		}
		mixed := processor.Mix(input, silent, float64(i)/200, 1)
		if i > 110 {
			late = math.Max(late, audiotest.BufferPeak(mixed))
		}
	}
	if late < 500 {
		t.Errorf("echo after the input stopped peaks at %.0f, want the repeats of audio sent while the wet was still closed", late)
	}
}

func TestNoiseRiserBuildsFromItsStart(t *testing.T) {
	for _, c := range []struct {
		fx    string
		start int
	}{{"noise", 100}, {"noise_at_end", 150}} {
		recipe := combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXOut, c.fx))
		processor := transition.NewProcessor(recipe, eightBarWindow(200))
		silent := make([]int16, dsp.FrameSize*dsp.Channels)

		for i := 0; i < 200; i++ {
			peak := audiotest.BufferPeak(processor.Mix(silent, silent, float64(i)/200, 1))
			if i < c.start && peak != 0 {
				t.Fatalf("%s frame %d carries noise at %.1f before its start", c.fx, i, peak)
			}
			if i > c.start+20 && peak < 500 {
				t.Fatalf("%s frame %d peaks at %.1f, want the riser audible", c.fx, i, peak)
			}
		}
	}
}

func TestLegacyNoiseRiserBecomesTheOutgoingNoise(t *testing.T) {
	recipe := combinedRecipe(filterStyle("noise_riser"))
	if recipe.Out.FX != transition.FXNoise || recipe.Out.Filter != transition.FilterNone || recipe.In.Filter != transition.FilterNone {
		t.Errorf("noise_riser expands to %s, want fx_out noise and no filters", recipe)
	}
}

func jogPeaks(loop string, frames int) []float64 {
	recipe := combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryLoop, loop))
	processor := transition.NewProcessor(recipe, eightBarWindow(frames))
	tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	peaks := make([]float64, frames)
	for i := range peaks {
		tone.Fill(frame)
		peaks[i] = audiotest.BufferPeak(processor.Mix(frame, silent, float64(i)/float64(frames), 1))
	}
	return peaks
}

func TestVinylStopEndsInSilence(t *testing.T) {
	peaks := jogPeaks("vinyl_stop_end", 200)
	if peaks[50] < 8000 {
		t.Errorf("outgoing peak before the stop = %.0f, want the song playing", peaks[50])
	}
	if peaks[199] > peaks[50]/100 {
		t.Errorf("last frame peaks at %.1f, want the record stopped", peaks[199])
	}
}

func TestVinylStopAtCenterStaysSilentAfterTheMiddle(t *testing.T) {
	for _, loop := range []string{"vinyl_stop_center", "vinyl_stop_center_short"} {
		peaks := jogPeaks(loop, 400)
		if peaks[10] < 8000 {
			t.Errorf("%s peak before the stop = %.0f, want the song playing", loop, peaks[10])
		}
		for i := 205; i < 400; i++ {
			if peaks[i] > 1 {
				t.Fatalf("%s frame %d peaks at %.1f, want silence after the stop at the middle", loop, i, peaks[i])
			}
		}
	}
}

func TestShortVinylStopStartsLater(t *testing.T) {
	long := jogPeaks("vinyl_stop_end", 400)
	short := jogPeaks("vinyl_stop_end_short", 400)
	quietLong, quietShort := 0, 0
	for i := 300; i < 400; i++ {
		if long[i] < 4000 {
			quietLong++
		}
		if short[i] < 4000 {
			quietShort++
		}
	}
	if quietShort >= quietLong {
		t.Errorf("short stop is quiet for %d of the last frames against %d for the long one, want it to start later", quietShort, quietLong)
	}
}

func TestJogwheelStaysFinitePastItsStop(t *testing.T) {
	for _, loop := range []string{"spinback_two_beats", "vinyl_stop_end", "vinyl_stop_center"} {
		recipe := combinedRecipe(volumeStyle("crossfade"), sideStyle(transition.CategoryLoop, loop))
		processor := transition.NewProcessor(recipe, eightBarWindow(200))
		tone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 9000}
		frame := make([]int16, dsp.FrameSize*dsp.Channels)
		for i := 0; i < 230; i++ {
			tone.Fill(frame)
			if out := processor.Fade(frame, math.Min(float64(i)/200, 0.999), 1); !audiotest.IsBufferFinite(out) {
				t.Fatalf("%s frame %d is not finite once the record has stopped", loop, i)
			}
		}
	}
}

func TestSpinbackRacesBackwards(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), effectStyle("spinback_one_beat")), eightBarWindow(200))
	tone := &audiotest.ToneGenerator{Frequency: 100, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	crossings := func(buf []float64) int {
		count := 0
		for i := dsp.Channels; i < len(buf); i += dsp.Channels {
			if (buf[i] >= 0) != (buf[i-dsp.Channels] >= 0) {
				count++
			}
		}
		return count
	}
	var before int
	during := map[int]int{}
	for i := 0; i < 200; i++ {
		tone.Fill(frame)
		mixed := processor.Mix(frame, silent, float64(i)/200, 1)
		switch {
		case i == 150:
			before = crossings(mixed)
		case i >= 178 && i <= 186:
			during[i] = crossings(mixed)
		}
	}
	for i, count := range during {
		if count < before*3 {
			t.Errorf("frame %d has %d zero crossings against %d before, want the spinback still racing", i, count, before)
		}
	}
}

func runTransitionWindow(recipe *transition.Recipe, window *transition.Window) (bool, float64) {
	processor := transition.NewProcessor(recipe, window)
	aTone := &audiotest.ToneGenerator{Frequency: 220, Amplitude: 8000}
	bTone := &audiotest.ToneGenerator{Frequency: 660, Amplitude: 8000}
	aFrame := make([]int16, dsp.FrameSize*dsp.Channels)
	bFrame := make([]int16, dsp.FrameSize*dsp.Channels)

	maxJump := 0.0
	previous := 0.0
	for frame := 0; frame < window.Frames; frame++ {
		aTone.Fill(aFrame)
		bTone.Fill(bFrame)
		mixed := processor.Mix(aFrame, bFrame, float64(frame)/float64(window.Frames), 1)
		if !audiotest.IsBufferFinite(mixed) {
			return false, maxJump
		}
		for i := 0; i < len(mixed); i += dsp.Channels {
			maxJump = math.Max(maxJump, math.Abs(mixed[i]-previous))
			previous = mixed[i]
		}
	}
	return true, maxJump
}

func TestEveryShapeCombinationStaysFiniteAndClickFree(t *testing.T) {
	for _, volume := range legacyNames(transition.ShortcutVolume) {
		for _, eq := range legacyNames(transition.ShortcutEQ) {
			for _, filter := range legacyNames(transition.ShortcutFilter) {
				recipe := combinedRecipe(volumeStyle(volume), eqStyle(eq), filterStyle(filter))
				finite, jump := runTransitionWindow(recipe, eightBarWindow(12))
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

func TestEverySideStyleStaysFiniteAndClickFree(t *testing.T) {
	for _, category := range transition.StyleCategories() {
		for _, style := range legacyNames(category) {
			recipe := combinedRecipe(volumeStyle("crossfade"), sideStyle(category, style))
			finite, jump := runTransitionWindow(recipe, eightBarWindow(150))
			if !finite {
				t.Errorf("%s %s produced non-finite audio", category, style)
			}
			if jump > 8000 {
				t.Errorf("%s %s jumped %.0f between samples, want at most 8000", category, style, jump)
			}
		}
	}
}

func TestEveryEffectStaysFiniteAndClickFree(t *testing.T) {
	for _, volume := range legacyNames(transition.ShortcutVolume) {
		for _, effect := range legacyNames(transition.ShortcutEffect) {
			recipe := combinedRecipe(volumeStyle(volume), effectStyle(effect))
			finite, jump := runTransitionWindow(recipe, eightBarWindow(150))
			if !finite {
				t.Errorf("%s produced non-finite audio", recipe)
			}
			if jump > 8000 {
				t.Errorf("%s jumped %.0f between samples, want at most 8000", recipe, jump)
			}
		}
	}
}

func TestDegenerateWindowsStayFinite(t *testing.T) {
	recipe := combinedRecipe(volumeStyle("cut"), eqStyle("quick_bass"), effectStyle("spinback_four_beats"),
		sideStyle(transition.CategoryFXOut, "noise"), sideStyle(transition.CategoryFXIn, "phaser"),
		sideStyle(transition.CategoryVolumeIn, "switcharoo"))
	for _, window := range []*transition.Window{
		{Frames: 1, PeriodSec: 0.5, Bars: 2},
		{Frames: 2, PeriodSec: 0.5},
		{Frames: 200},
		{Frames: 200, PeriodSec: -1, Bars: -4},
		{Frames: 200, PeriodSec: 30, Bars: 16},
		{Frames: 200, PeriodSec: 0.01, Bars: 1},
	} {
		if finite, _ := runTransitionWindow(recipe, window); !finite {
			t.Errorf("window %+v produced non-finite audio", window)
		}
	}
}

func TestNilFramesAreSilence(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(), eightBarWindow(100))

	if peak := audiotest.BufferPeak(processor.Mix(nil, nil, 0.5, 1)); peak != 0 {
		t.Errorf("peak = %.1f, want 0", peak)
	}
}

func TestOutroFadeStartsAtThePlayingLevelAndEndsSilent(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("crossfade")), eightBarWindow(200))
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for i := range frame {
		frame[i] = 10000
	}

	first := processor.Fade(frame, 0, 0.5)[0]
	var last float64
	for i := 1; i <= 200; i++ {
		buf := processor.Fade(frame, float64(i)/200, 0.5)
		last = buf[len(buf)-1]
	}
	if math.Abs(first-5000) > 1 {
		t.Errorf("first outro sample = %.1f, want 5000 at half volume", first)
	}
	if last > 1 {
		t.Errorf("last outro sample = %.1f, want silence", last)
	}
}
