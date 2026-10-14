package dsp

import "math"

const (
	Channels   = 2
	SampleRate = 48000
	FrameSize  = 960
	FullScale  = 32767.0

	FramesPerSecond = float64(SampleRate) / float64(FrameSize)

	filterMinFreq      = 20.0
	filterMaxFreqRatio = 0.45

	workSamples = 64
	workLength  = workSamples * Channels
)

func clampFrequency(freq float64) float64 {
	if freq < filterMinFreq {
		return filterMinFreq
	}
	maxFreq := SampleRate * filterMaxFreqRatio
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

type workBuffer [workLength]float64

type filterCoefficients struct {
	b0, b1, b2, a1, a2 float64
}

type filterState struct {
	z1, z2 float64
}

type Filter struct {
	filterCoefficients
	states [Channels]filterState
	bypass bool
}

func (c filterCoefficients) next(state filterState, input float64) (filterState, float64) {
	output := c.b0*input + state.z1
	z1 := c.b1*input - c.a1*output + state.z2
	z2 := c.b2*input - c.a2*output
	return filterState{z1: z1, z2: z2}, output
}

func (f *Filter) SetBypass() {
	f.b0, f.b1, f.b2, f.a1, f.a2 = 1, 0, 0, 0, 0
	f.bypass = true
}

func (f *Filter) Normalize(b0, b1, b2, a0, a1, a2 float64) {
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

func filterTerms(freq, q float64) (float64, float64) {
	if q <= 0 {
		q = 0.707
	}
	w0 := 2 * math.Pi * clampFrequency(freq) / SampleRate
	return math.Cos(w0), math.Sin(w0) / (2 * q)
}

func (f *Filter) SetLowpass(freq, q float64) {
	cosW, alpha := filterTerms(freq, q)
	f.Normalize((1-cosW)/2, 1-cosW, (1-cosW)/2, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Filter) SetHighpass(freq, q float64) {
	cosW, alpha := filterTerms(freq, q)
	f.Normalize((1+cosW)/2, -(1 + cosW), (1+cosW)/2, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Filter) SetBandpass(freq, q float64) {
	cosW, alpha := filterTerms(freq, q)
	f.Normalize(alpha, 0, -alpha, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Filter) SetAllpass(freq, q float64) {
	cosW, alpha := filterTerms(freq, q)
	f.Normalize(1-alpha, -2*cosW, 1+alpha, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Filter) Reset() {
	f.states = [Channels]filterState{}
}

func (f *Filter) ProcessStereo(buf []float64) {
	if f.bypass {
		return
	}
	coefficients := f.filterCoefficients
	var work workBuffer
	for start := 0; start+1 < len(buf); start += workLength {
		chunk := buf[start:min(start+workLength, len(buf))]
		end := copy(work[:], chunk) &^ 1
		for channel := range f.states {
			state := f.states[channel]
			for i := channel; i < end; i += Channels {
				state, work[i] = coefficients.next(state, work[i])
			}
			f.states[channel] = state
		}
		copy(chunk, work[:end])
	}
}

func (f *Filter) step(channel int, input float64) float64 {
	var output float64
	f.states[channel], output = f.next(f.states[channel], input)
	return output
}

func FrameToFloat(src []int16, dst []float64) {
	var samples [workLength]int16
	var work workBuffer
	for start := 0; start < len(dst); start += workLength {
		chunk := dst[start:min(start+workLength, len(dst))]
		available := copy(samples[:len(chunk)], src[min(start, len(src)):])
		for i := range len(chunk) {
			if i < available {
				work[i] = float64(samples[i])
			} else {
				work[i] = 0
			}
		}
		copy(chunk, work[:len(chunk)])
	}
}

func FloatToFrame(src []float64, dst []int16) {
	var work workBuffer
	var samples [workLength]int16
	for start := 0; start < len(dst); start += workLength {
		chunk := dst[start:min(start+workLength, len(dst))]
		available := copy(work[:len(chunk)], src[min(start, len(src)):])
		for i := range len(chunk) {
			var sample float64
			if i < available {
				sample = work[i]
			}
			samples[i] = ClampToInt16(sample)
		}
		copy(chunk, samples[:len(chunk)])
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

func AddBuffers(dst, a, b []float64) {
	var sums, addends workBuffer
	for start := 0; start < len(dst); start += workLength {
		chunk := dst[start:min(start+workLength, len(dst))]
		count := copy(sums[:len(chunk)], a[start:])
		copy(addends[:count], b[start:])
		for i := range count {
			sums[i] += addends[i]
		}
		copy(chunk, sums[:count])
	}
}

func MixWet(block, wet []float64, fromDry, toDry, fromWet, toWet float64) {
	steps := len(block) / Channels
	var dryWork, wetWork workBuffer
	for start := 0; start+1 < len(block); start += workLength {
		chunk := block[start:min(start+workLength, len(block))]
		count := copy(dryWork[:], chunk)
		copy(wetWork[:count], wet[start:])
		for i := 0; i+1 < count; i += Channels {
			blend := float64((start+i)/Channels+1) / float64(steps)
			dryGain := fromDry + (toDry-fromDry)*blend
			wetGain := fromWet + (toWet-fromWet)*blend
			dryWork[i] = dryWork[i]*dryGain + wetWork[i]*wetGain
			dryWork[i+1] = dryWork[i+1]*dryGain + wetWork[i+1]*wetGain
		}
		copy(chunk, dryWork[:count])
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
