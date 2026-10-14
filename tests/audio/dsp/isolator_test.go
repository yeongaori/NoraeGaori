package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func isolatorResponse(levels [3]float64, frequency float64) float64 {
	var isolator dsp.Isolator
	return audiotest.Decibels(audiotest.ToneResponse(func(buf []float64) { isolator.Process(buf, levels) }, frequency))
}

func TestIsolatorAtUnityIsFlat(t *testing.T) {
	for _, frequency := range []float64{50, 300, 1000, 4000, 12000} {
		if gain := isolatorResponse([3]float64{0.5, 0.5, 0.5}, frequency); math.Abs(gain) > 0.3 {
			t.Errorf("%.0fHz = %+.2f dB, want within 0.3 dB of flat", frequency, gain)
		}
	}
}

func TestIsolatorKillsTheMidBandLikeTheMeasuredMixer(t *testing.T) {
	levels := [3]float64{0.5, 0, 0.5}

	if gain := isolatorResponse(levels, 1000); gain > -30 {
		t.Errorf("1kHz = %+.1f dB, want below -30 dB", gain)
	}
	for _, frequency := range []float64{300, 4000} {
		if gain := isolatorResponse(levels, frequency); math.Abs(gain+6) > 2 {
			t.Errorf("%.0fHz = %+.1f dB, want about -6 dB at the crossover", frequency, gain)
		}
	}
	if gain := isolatorResponse(levels, 50); math.Abs(gain) > 0.5 {
		t.Errorf("50Hz = %+.1f dB, want the low band untouched", gain)
	}
}

func TestIsolatorKillsTheLowBand(t *testing.T) {
	levels := [3]float64{0, 0.5, 0.5}

	if gain := isolatorResponse(levels, 50); gain > -30 {
		t.Errorf("50Hz = %+.1f dB, want below -30 dB", gain)
	}
	if gain := isolatorResponse(levels, 2000); math.Abs(gain) > 0.5 {
		t.Errorf("2kHz = %+.1f dB, want untouched", gain)
	}
}

func TestIsolatorKeepsASilentChannelSilent(t *testing.T) {
	var isolator dsp.Isolator
	block := make([]float64, dsp.FrameSize*dsp.Channels)
	for frame := 0; frame < 5; frame++ {
		for i := 0; i < len(block); i += dsp.Channels {
			block[i] = 9000 * math.Sin(2*math.Pi*200*float64(frame*dsp.FrameSize+i/dsp.Channels)/dsp.SampleRate)
			block[i+1] = 0
		}
		isolator.Process(block, [3]float64{0.8, 0.3, 0.6})
		for i := 1; i < len(block); i += dsp.Channels {
			if block[i] != 0 {
				t.Fatalf("frame %d right sample %d = %v, want the silent channel untouched by the left", frame, i, block[i])
			}
		}
	}
}

func TestIsolatorRampsTheGainsAcrossTheWholeBuffer(t *testing.T) {
	var isolator dsp.Isolator
	phase := 0.0
	isolator.Process(audiotest.SineFloatFrame(1000, 9000, &phase), [3]float64{0.5, 0.5, 0.5})
	block := audiotest.SineFloatFrame(1000, 9000, &phase)
	isolator.Process(block, [3]float64{0, 0, 0})

	window := 64 * dsp.Channels
	first := audiotest.BufferRMS(block[:window])
	last := audiotest.BufferRMS(block[len(block)-window:])
	if last > first/10 {
		t.Errorf("last 64 samples at %.0f RMS against %.0f at the start, want the kill to ramp down over the whole frame", last, first)
	}
	if end := block[len(block)-dsp.Channels:]; end[0] != 0 || end[1] != 0 {
		t.Errorf("last sample pair = %v, want the kill reached exactly on the final sample", end)
	}
}

func TestIsolatorHighBandSitsAboveBothCrossovers(t *testing.T) {
	if gain := isolatorResponse([3]float64{0, 0, 0.5}, 100); gain > -150 {
		t.Errorf("100Hz through the high band alone = %+.1f dB, want below -150 dB from both crossover slopes", gain)
	}
}

func TestEQTaperMatchesTheMeasuredKnobs(t *testing.T) {
	cases := []struct{ level, decibels float64 }{
		{0.05, -26.4}, {0.2, -11.7}, {0.35, -3.8}, {0.5, 0}, {0.75, 2.0}, {1, 6.2}, {0.4, -2.775},
	}
	for _, want := range cases {
		if got := audiotest.Decibels(dsp.EQGain(want.level)); math.Abs(got-want.decibels) > 0.01 {
			t.Errorf("EQGain(%.2f) = %+.2f dB, want %+.2f dB", want.level, got, want.decibels)
		}
	}
	if got := dsp.EQGain(0); got != 0 {
		t.Errorf("EQGain(0) = %v, want a full kill", got)
	}
	if got := dsp.EQGain(0.025); math.Abs(got-dsp.EQGain(0.05)/2) > 1e-9 {
		t.Errorf("EQGain(0.025) = %v, want half of the first knob", got)
	}
}
