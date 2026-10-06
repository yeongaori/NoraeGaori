package dsp

import "math"

const (
	Channels   = 2
	SampleRate = 48000
	FrameSize  = 960
	FullScale  = 32767.0

	FramesPerSecond = float64(SampleRate) / float64(FrameSize)

	biquadMinFreq      = 20.0
	biquadMaxFreqRatio = 0.45
)

func clampFrequency(freq float64) float64 {
	if freq < biquadMinFreq {
		return biquadMinFreq
	}
	maxFreq := SampleRate * biquadMaxFreqRatio
	if freq > maxFreq {
		return maxFreq
	}
	return freq
}

func ClampUnit(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

func SmoothStep(v float64) float64 {
	v = ClampUnit(v)
	return v * v * (3 - 2*v)
}

type Biquad struct {
	b0, b1, b2, a1, a2 float64
	leftZ1, leftZ2     float64
	rightZ1, rightZ2   float64
	bypass             bool
}

func (f *Biquad) SetBypass() {
	f.b0, f.b1, f.b2, f.a1, f.a2 = 1, 0, 0, 0, 0
	f.bypass = true
}

func (f *Biquad) Normalize(b0, b1, b2, a0, a1, a2 float64) {
	if a0 == 0 {
		f.SetBypass()
		return
	}
	f.b0 = b0 / a0
	f.b1 = b1 / a0
	f.b2 = b2 / a0
	f.a1 = a1 / a0
	f.a2 = a2 / a0
	f.bypass = false
}

func biquadTerms(freq, q float64) (float64, float64) {
	if q <= 0 {
		q = 0.707
	}
	w0 := 2 * math.Pi * clampFrequency(freq) / SampleRate
	return math.Cos(w0), math.Sin(w0) / (2 * q)
}

func (f *Biquad) SetLowpass(freq, q float64) {
	cosW, alpha := biquadTerms(freq, q)
	f.Normalize((1-cosW)/2, 1-cosW, (1-cosW)/2, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Biquad) SetHighpass(freq, q float64) {
	cosW, alpha := biquadTerms(freq, q)
	f.Normalize((1+cosW)/2, -(1 + cosW), (1+cosW)/2, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Biquad) SetBandpass(freq, q float64) {
	cosW, alpha := biquadTerms(freq, q)
	f.Normalize(alpha, 0, -alpha, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Biquad) SetAllpass(freq, q float64) {
	cosW, alpha := biquadTerms(freq, q)
	f.Normalize(1-alpha, -2*cosW, 1+alpha, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Biquad) Reset() {
	f.leftZ1, f.leftZ2, f.rightZ1, f.rightZ2 = 0, 0, 0, 0
}

func (f *Biquad) ProcessStereo(buf []float64) {
	if f.bypass {
		return
	}
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = f.stepLeft(buf[i])
		buf[i+1] = f.stepRight(buf[i+1])
	}
}

func (f *Biquad) stepLeft(input float64) float64 {
	output := f.b0*input + f.leftZ1
	f.leftZ1 = f.b1*input - f.a1*output + f.leftZ2
	f.leftZ2 = f.b2*input - f.a2*output
	return output
}

func (f *Biquad) stepRight(input float64) float64 {
	output := f.b0*input + f.rightZ1
	f.rightZ1 = f.b1*input - f.a1*output + f.rightZ2
	f.rightZ2 = f.b2*input - f.a2*output
	return output
}

func FrameToFloat(src []int16, dst []float64) {
	for i := range dst {
		if i < len(src) {
			dst[i] = float64(src[i])
		} else {
			dst[i] = 0
		}
	}
}

func FloatToFrame(src []float64, dst []int16) {
	for i := range dst {
		var sample float64
		if i < len(src) {
			sample = src[i]
		}
		dst[i] = ClampToInt16(sample)
	}
}

func ClampToInt16(sample float64) int16 {
	if sample > 32767 {
		return 32767
	}
	if sample < -32768 {
		return -32768
	}
	return int16(sample)
}

func ApplyGainRamp(buf []float64, from, to float64) {
	if from == to {
		if from == 1 {
			return
		}
		for i := range buf {
			buf[i] *= from
		}
		return
	}
	steps := len(buf) / Channels
	if steps < 1 {
		return
	}
	delta := (to - from) / float64(steps)
	gain := from
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] *= gain
		buf[i+1] *= gain
		gain += delta
	}
}

func DryWet(level float64) (float64, float64) {
	angle := ClampUnit(level) * math.Pi / 2
	return math.Sqrt(math.Cos(angle)), math.Sqrt(math.Sin(angle))
}

func SilenceFloat(buf []float64) {
	for i := range buf {
		buf[i] = 0
	}
}

func QSinIn(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	return math.Sin(p * math.Pi / 2)
}

func QSinOut(p float64) float64 {
	if p <= 0 {
		return 1
	}
	if p >= 1 {
		return 0
	}
	return math.Cos(p * math.Pi / 2)
}
