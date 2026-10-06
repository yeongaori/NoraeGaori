package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestNoiseRiserSweepsTheMeasuredBand(t *testing.T) {
	cases := []struct{ colour, frequency float64 }{{0.62, 457}, {0.83, 6430}}
	for _, want := range cases {
		if got := dsp.NoiseRiserFrequency(want.colour); math.Abs(got-want.frequency) > want.frequency*0.02 {
			t.Errorf("colour %.2f centres at %.0f Hz, want about %.0f Hz", want.colour, got, want.frequency)
		}
	}
}

func TestNoiseRiserIsSilentAtRest(t *testing.T) {
	var riser dsp.NoiseRiser
	buf := make([]float64, dsp.FrameSize*dsp.Channels)
	riser.Render(buf, dsp.KnobCenter)

	if peak := audiotest.BufferPeak(buf); peak != 0 {
		t.Errorf("riser at rest peaks at %.3f, want silence", peak)
	}
}

func TestNoiseRiserHoldsItsLevelAcrossTheSweep(t *testing.T) {
	for _, colour := range []float64{0.65, 0.75, 0.83} {
		var riser dsp.NoiseRiser
		buf := make([]float64, dsp.FrameSize*dsp.Channels)
		var rms float64
		for frame := 0; frame < 20; frame++ {
			riser.Render(buf, colour)
			rms = audiotest.BufferRMS(buf)
		}
		if level := audiotest.Decibels(rms / 1640); math.Abs(level) > 2 {
			t.Errorf("colour %.2f level %+.1f dB against the target, want within 2 dB", colour, level)
		}
	}
}

func TestNoiseRiserChannelsAreIndependent(t *testing.T) {
	var riser dsp.NoiseRiser
	buf := make([]float64, dsp.FrameSize*dsp.Channels)
	var leftRight, leftLeft, rightRight float64
	for frame := 0; frame < 20; frame++ {
		riser.Render(buf, 0.75)
		for i := 0; i+1 < len(buf); i += dsp.Channels {
			leftRight += buf[i] * buf[i+1]
			leftLeft += buf[i] * buf[i]
			rightRight += buf[i+1] * buf[i+1]
		}
	}
	if correlation := leftRight / math.Sqrt(leftLeft*rightRight); math.Abs(correlation) > 0.2 {
		t.Errorf("left/right correlation %.3f, want independent noise", correlation)
	}
}
