package transition_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

func TestSwitcharooGatesMatchTheClientPattern(t *testing.T) {
	gates := map[int]int{2: 24, 4: 30, 8: 38, 16: 54, 32: 86}
	for bars, want := range gates {
		recipe := combinedRecipe(sideStyle(transition.CategoryVolumeOut, "switcharoo"), sideStyle(transition.CategoryVolumeIn, "switcharoo"))
		processor := transition.NewProcessor(recipe, &transition.Window{Frames: 1000, Bars: bars})

		out, in := processor.Gains(0)
		if out != 0 || in != 1 {
			t.Errorf("%d bars start at (%.0f, %.0f), want the outgoing gate closed and the incoming open", bars, out, in)
		}
		changes := 0
		previous := out
		for step := 1; step <= 40000; step++ {
			out, in = processor.Gains(float64(step) / 40000)
			if out+in != 1 || (out != 0 && out != 1) {
				t.Fatalf("%d bars at %.5f = (%.3f, %.3f), want complementary hard gates", bars, float64(step)/40000, out, in)
			}
			if out != previous {
				changes++
				previous = out
			}
		}
		if changes != want-1 {
			t.Errorf("%d bars switch %d times, want the %d gates of the client pattern", bars, changes, want)
		}
	}
}

func TestSwitcharooQuarterBeatGatesFillTheLastBar(t *testing.T) {
	recipe := combinedRecipe(sideStyle(transition.CategoryVolumeOut, "switcharoo"))
	processor := transition.NewProcessor(recipe, &transition.Window{Frames: 1000, Bars: 8})
	gate := 1.0 / 128
	for index := 0; index < 16; index++ {
		middle := 0.875 + (float64(index)+0.5)*gate
		want := float64(index % 2)
		if out, _ := processor.Gains(middle); out != want {
			t.Errorf("quarter-beat gate %d = %.0f, want %.0f", index, out, want)
		}
	}
}

func lowLevel(choice styleChoice, outgoing bool, progress float64) float64 {
	return steadyLevel(shaped(choice), outgoing, 50, progress)
}

func TestBassFadeCutsTheOutgoingLowsEarly(t *testing.T) {
	fade := sideStyle(transition.CategoryEQOut, "bass_fade")
	if level := lowLevel(fade, true, 0.02); math.Abs(level) > 0.5 {
		t.Errorf("outgoing 50Hz at the start = %+.1f dB, want untouched", level)
	}
	if level := lowLevel(fade, true, 0.55); level > -30 {
		t.Errorf("outgoing 50Hz past the middle = %+.1f dB, want killed", level)
	}
	if lowLevel(fade, true, 0.2) <= lowLevel(fade, true, 0.45) {
		t.Error("outgoing bass is no louder early in the fade than late, want it falling")
	}
}

func TestBassFadeHoldsMostOfTheBassAtAQuarter(t *testing.T) {
	level := lowLevel(sideStyle(transition.CategoryEQOut, "bass_fade"), true, 0.25)
	if math.Abs(level+2.9) > 1.2 {
		t.Errorf("outgoing 50Hz at a quarter = %+.1f dB, want about -2.9 dB from the late knee of the client curve", level)
	}
}

func TestBassCrossfadeMeetsAtTheMiddle(t *testing.T) {
	for _, outgoing := range []bool{true, false} {
		category := transition.CategoryEQIn
		if outgoing {
			category = transition.CategoryEQOut
		}
		level := lowLevel(sideStyle(category, "bass_crossfade"), outgoing, 0.5)
		if math.Abs(level+8.2) > 1 {
			t.Errorf("outgoing=%t 50Hz at the middle = %+.1f dB, want the measured -8.2 dB of the half-way knob", outgoing, level)
		}
	}
}

func TestSingleBandSwapsLeaveTheOtherBands(t *testing.T) {
	cases := []struct {
		style     string
		frequency float64
	}{
		{"mid_fast", 1000},
		{"hi_fast", 12000},
	}
	for _, c := range cases {
		processor := func() *transition.Processor { return shaped(sideStyle(transition.CategoryEQOut, c.style)) }
		if level := steadyLevel(processor(), true, c.frequency, 0.4); math.Abs(level) > 0.5 {
			t.Errorf("%s before the swap = %+.1f dB, want untouched", c.style, level)
		}
		if level := steadyLevel(processor(), true, c.frequency, 0.6); math.Abs(level+11.7) > 1.5 {
			t.Errorf("%s after the swap = %+.1f dB, want the measured -11.7 dB cut", c.style, level)
		}
		if level := steadyLevel(processor(), true, 50, 0.6); math.Abs(level) > 0.5 {
			t.Errorf("%s moves the bass by %+.1f dB, want only its own band", c.style, level)
		}
	}
}

func outgoingThrough(fx string, frames int, progressAt func(int) float64) [][]float64 {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXOut, fx)), eightBarWindow(frames))
	tone := &audiotest.ToneGenerator{Frequency: 1000, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	outputs := make([][]float64, frames)
	for i := range outputs {
		tone.Fill(frame)
		outputs[i] = append([]float64(nil), processor.Mix(frame, silent, progressAt(i), 1)...)
		for j := range outputs[i] {
			outputs[i][j] -= float64(frame[j])
		}
	}
	return outputs
}

func TestInsertEffectsStartAtTheMiddle(t *testing.T) {
	for _, fx := range []string{"phaser", "bitcrusher"} {
		changes := outgoingThrough(fx, 200, func(i int) float64 { return float64(i) / 200 })
		for i := 0; i < 99; i++ {
			if peak := audiotest.BufferPeak(changes[i]); peak > 1e-6 {
				t.Fatalf("%s frame %d moves the dry signal by %.3f, want it untouched before the middle", fx, i, peak)
			}
		}
		if peak := audiotest.BufferPeak(changes[180]); peak < 500 {
			t.Errorf("%s late in the overlap changes the signal by only %.1f, want it audible", fx, peak)
		}
	}
}

func TestDelayRampAndOneBarDelayWaitForTheirWindow(t *testing.T) {
	for _, c := range []struct {
		fx    string
		start int
	}{{"delay_ramp", 100}, {"delay_one_bar_at_end", 150}, {"delay_ramp_at_end", 150}} {
		changes := outgoingThrough(c.fx, 200, func(i int) float64 { return float64(i) / 200 })
		for i := 0; i < c.start-1; i++ {
			if peak := audiotest.BufferPeak(changes[i]); peak > 1e-6 {
				t.Fatalf("%s frame %d moves the dry signal by %.3f, want it untouched before its window", c.fx, i, peak)
			}
		}
		if peak := audiotest.BufferPeak(changes[c.start+30]); peak < 500 {
			t.Errorf("%s inside its window changes the signal by only %.1f, want it audible", c.fx, peak)
		}
	}
}

func TestDelayRampShortensTheRepeats(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXOut, "delay_ramp")), eightBarWindow(400))
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	click := make([]int16, dsp.FrameSize*dsp.Channels)
	click[0], click[1] = 20000, 20000

	clickFrame := 250
	firstEcho := -1
	for i := 0; i < 400 && firstEcho < 0; i++ {
		input := silent
		if i == clickFrame {
			input = click
		}
		mixed := processor.Mix(input, silent, float64(i)/400, 1)
		if i <= clickFrame {
			continue
		}
		for j := 0; j < len(mixed); j += dsp.Channels {
			if math.Abs(mixed[j]) > 500 {
				firstEcho = (i-clickFrame)*dsp.FrameSize + j/dsp.Channels
				break
			}
		}
	}
	if beat := 24000; firstEcho < 0 || firstEcho > beat*3/4 {
		t.Errorf("first repeat came %d samples after the click, want well inside the one-beat %d the ramp starts from", firstEcho, beat)
	}
}

func TestIncomingDelayAtEndIsCutAtTheEnd(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXIn, "delay_one_bar_at_end")), eightBarWindow(200))
	tone := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	var mixed []float64
	for i := 0; i < 200; i++ {
		tone.Fill(frame)
		mixed = processor.Mix(silent, frame, float64(i)/200, 1)
	}
	last := len(mixed) - dsp.Channels
	if math.Abs(mixed[last]-float64(frame[last])) > 1 {
		t.Errorf("the last incoming sample = %.1f, want the dry %d once the delay is cut at the end", mixed[last], frame[last])
	}
}

func TestIncomingOneBarDelayEchoesTheFirstBar(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXIn, "delay_one_bar")), eightBarWindow(800))
	tone := &audiotest.ToneGenerator{Frequency: 440, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	echo := 0.0
	for i := 0; i < 400; i++ {
		input := silent
		if i < 50 {
			tone.Fill(frame)
			input = frame
		}
		mixed := processor.Mix(silent, input, float64(i)/800, 1)
		switch {
		case i >= 60 && i < 95 && audiotest.BufferPeak(mixed) > 1:
			t.Fatalf("frame %d carries %.1f before the echo is due", i, audiotest.BufferPeak(mixed))
		case i >= 105 && i < 145:
			echo = math.Max(echo, audiotest.BufferPeak(mixed))
		}
	}
	if echo < 1000 {
		t.Errorf("the echo of the incoming first bar peaks at %.0f, want it audible one bar later", echo)
	}
}

func TestIncomingPhaserFadesOutByTheMiddle(t *testing.T) {
	processor := transition.NewProcessor(combinedRecipe(volumeStyle("overlap"), sideStyle(transition.CategoryFXIn, "phaser")), eightBarWindow(200))
	tone := &audiotest.ToneGenerator{Frequency: 1000, Amplitude: 9000}
	silent := make([]int16, dsp.FrameSize*dsp.Channels)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)

	early := 0.0
	for i := 0; i < 200; i++ {
		tone.Fill(frame)
		mixed := processor.Mix(silent, frame, float64(i)/200, 1)
		change := 0.0
		for j := range mixed {
			change = math.Max(change, math.Abs(mixed[j]-float64(frame[j])))
		}
		if i == 20 {
			early = change
		}
		if i > 101 && change > 1e-6 {
			t.Fatalf("frame %d still moves the incoming song by %.3f, want the phaser gone after the middle", i, change)
		}
	}
	if early < 500 {
		t.Errorf("phaser early in the overlap changes the incoming song by only %.1f, want it audible", early)
	}
}
