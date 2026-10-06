package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestKnobCutoffsSpanTheMeasuredRange(t *testing.T) {
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"low-pass closed", dsp.LowpassKnobFrequency(0), 120},
		{"low-pass open", dsp.LowpassKnobFrequency(0.5), 16000},
		{"low-pass quarter", dsp.LowpassKnobFrequency(0.25), 120 * math.Sqrt(16000.0/120)},
		{"high-pass open", dsp.HighpassKnobFrequency(0.5), 20},
		{"high-pass closed", dsp.HighpassKnobFrequency(1), 9000},
	}
	for _, c := range cases {
		if math.Abs(c.got-c.want) > c.want*1e-9 {
			t.Errorf("%s = %.2f Hz, want %.2f Hz", c.name, c.got, c.want)
		}
	}
}

func TestKnobResonanceFollowsTheMeasuredTable(t *testing.T) {
	cases := []struct{ knob, q float64 }{
		{0.48, 0.70}, {0.35, 2.20}, {0.05, 0.65}, {0.01, 0.65}, {0.335, 2.15},
		{0.52, 0.70}, {0.85, 1.95}, {0.98, 0.80}, {1, 0.80},
	}
	for _, want := range cases {
		if got := dsp.KnobQ(want.knob); math.Abs(got-want.q) > 1e-9 {
			t.Errorf("KnobQ(%.3f) = %.3f, want %.3f", want.knob, got, want.q)
		}
	}
}

func TestCentredKnobIsTransparent(t *testing.T) {
	for _, knob := range []float64{0.499, 0.5, 0.501} {
		for _, frequency := range []float64{50, 15000} {
			gain := audiotest.FilterResponse(func(f *dsp.Biquad) { f.SetKnob(knob) }, frequency)
			if math.Abs(gain-1) > 1e-9 {
				t.Errorf("knob %.3f at %.0fHz gain = %.6f, want bypass", knob, frequency, gain)
			}
		}
	}
}

func TestKnobBelowCentreLowPasses(t *testing.T) {
	setup := func(f *dsp.Biquad) { f.SetKnob(0.25) }

	if gain := audiotest.FilterResponse(setup, 200); gain < 0.95 {
		t.Errorf("200Hz gain = %.3f, want the lows kept", gain)
	}
	if gain := audiotest.FilterResponse(setup, 10000); gain > 0.05 {
		t.Errorf("10kHz gain = %.3f, want the highs cut", gain)
	}
}

func TestKnobAboveCentreHighPasses(t *testing.T) {
	setup := func(f *dsp.Biquad) { f.SetKnob(0.75) }

	if gain := audiotest.FilterResponse(setup, 50); gain > 0.05 {
		t.Errorf("50Hz gain = %.3f, want the lows cut", gain)
	}
	if gain := audiotest.FilterResponse(setup, 5000); gain < 0.95 {
		t.Errorf("5kHz gain = %.3f, want the highs kept", gain)
	}
}
