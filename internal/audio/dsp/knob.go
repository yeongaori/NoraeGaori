package dsp

import "math"

const (
	KnobCenter         = 0.5
	knobBypassWidth    = 0.002
	lowpassClosedFreq  = 120.0
	lowpassOpenFreq    = 16000.0
	highpassOpenFreq   = 20.0
	highpassClosedFreq = 9000.0
)

var (
	lowpassQ = []Point{
		{0.05, 0.65}, {0.1, 0.75}, {0.15, 1.00}, {0.2, 1.35}, {0.25, 1.65}, {0.28, 1.95},
		{0.32, 2.10}, {0.35, 2.20}, {0.38, 2.10}, {0.42, 1.70}, {0.45, 1.05}, {0.48, 0.70},
	}
	highpassQ = []Point{
		{0.52, 0.70}, {0.55, 0.75}, {0.58, 0.75}, {0.62, 0.75}, {0.65, 1.00}, {0.7, 1.20},
		{0.75, 1.60}, {0.8, 1.90}, {0.85, 1.95}, {0.9, 1.75}, {0.95, 1.05}, {0.98, 0.80},
	}
)

func (f *Biquad) SetKnob(knob float64) {
	knob = ClampUnit(knob)
	switch {
	case math.Abs(knob-KnobCenter) <= knobBypassWidth:
		f.SetBypass()
	case knob < KnobCenter:
		f.SetLowpass(LowpassKnobFrequency(knob), KnobQ(knob))
	default:
		f.SetHighpass(HighpassKnobFrequency(knob), KnobQ(knob))
	}
}

func LowpassKnobFrequency(knob float64) float64 {
	return lowpassClosedFreq * math.Pow(lowpassOpenFreq/lowpassClosedFreq, 2*knob)
}

func HighpassKnobFrequency(knob float64) float64 {
	return highpassOpenFreq * math.Pow(highpassClosedFreq/highpassOpenFreq, 2*(knob-KnobCenter))
}

func KnobQ(knob float64) float64 {
	if knob < KnobCenter {
		return interpolate(lowpassQ, knob)
	}
	return interpolate(highpassQ, knob)
}
