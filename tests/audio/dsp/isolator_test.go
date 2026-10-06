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
