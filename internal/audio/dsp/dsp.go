package dsp

import "math"

const (
	Channels   = 2
	SampleRate = 48000
	FrameSize  = 960

	FramesPerSecond = float64(SampleRate) / float64(FrameSize)

	biquadMinFreq      = 20.0
	biquadMaxFreqRatio = 0.45
	reverbCombCount    = 4
	reverbAllpassCount = 2
	reverbDamping      = 0.35
	reverbRoomSize     = 0.84
	reverbStereoSpread = 23
	delayMaxSeconds    = 4.0
)

var (
	reverbCombLengths    = [reverbCombCount]int{1214, 1293, 1390, 1476}
	reverbAllpassLengths = [reverbAllpassCount]int{605, 480}
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

func (f *Biquad) SetLowpass(freq, q float64) {
	freq = clampFrequency(freq)
	if q <= 0 {
		q = 0.707
	}
	w0 := 2 * math.Pi * freq / SampleRate
	cosW := math.Cos(w0)
	alpha := math.Sin(w0) / (2 * q)
	f.Normalize((1-cosW)/2, 1-cosW, (1-cosW)/2, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Biquad) SetHighpass(freq, q float64) {
	freq = clampFrequency(freq)
	if q <= 0 {
		q = 0.707
	}
	w0 := 2 * math.Pi * freq / SampleRate
	cosW := math.Cos(w0)
	alpha := math.Sin(w0) / (2 * q)
	f.Normalize((1+cosW)/2, -(1 + cosW), (1+cosW)/2, 1+alpha, -2*cosW, 1-alpha)
}

func (f *Biquad) SetPeaking(freq, q, gainDB float64) {
	freq = clampFrequency(freq)
	if q <= 0 {
		q = 0.707
	}
	amplitude := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / SampleRate
	cosW := math.Cos(w0)
	alpha := math.Sin(w0) / (2 * q)
	f.Normalize(1+alpha*amplitude, -2*cosW, 1-alpha*amplitude,
		1+alpha/amplitude, -2*cosW, 1-alpha/amplitude)
}

func (f *Biquad) SetLowShelf(freq, q, gainDB float64) {
	freq = clampFrequency(freq)
	if q <= 0 {
		q = 0.707
	}
	amplitude := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / SampleRate
	cosW := math.Cos(w0)
	alpha := math.Sin(w0) / (2 * q)
	sqrtA := math.Sqrt(amplitude)
	f.Normalize(
		amplitude*((amplitude+1)-(amplitude-1)*cosW+2*sqrtA*alpha),
		2*amplitude*((amplitude-1)-(amplitude+1)*cosW),
		amplitude*((amplitude+1)-(amplitude-1)*cosW-2*sqrtA*alpha),
		(amplitude+1)+(amplitude-1)*cosW+2*sqrtA*alpha,
		-2*((amplitude-1)+(amplitude+1)*cosW),
		(amplitude+1)+(amplitude-1)*cosW-2*sqrtA*alpha,
	)
}

func (f *Biquad) SetHighShelf(freq, q, gainDB float64) {
	freq = clampFrequency(freq)
	if q <= 0 {
		q = 0.707
	}
	amplitude := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / SampleRate
	cosW := math.Cos(w0)
	alpha := math.Sin(w0) / (2 * q)
	sqrtA := math.Sqrt(amplitude)
	f.Normalize(
		amplitude*((amplitude+1)+(amplitude-1)*cosW+2*sqrtA*alpha),
		-2*amplitude*((amplitude-1)+(amplitude+1)*cosW),
		amplitude*((amplitude+1)+(amplitude-1)*cosW-2*sqrtA*alpha),
		(amplitude+1)-(amplitude-1)*cosW+2*sqrtA*alpha,
		2*((amplitude-1)-(amplitude+1)*cosW),
		(amplitude+1)-(amplitude-1)*cosW-2*sqrtA*alpha,
	)
}

func (f *Biquad) Reset() {
	f.leftZ1, f.leftZ2, f.rightZ1, f.rightZ2 = 0, 0, 0, 0
}

func (f *Biquad) ProcessStereo(buf []float64) {
	if f.bypass {
		return
	}
	for i := 0; i+1 < len(buf); i += 2 {
		left := buf[i]
		leftOut := f.b0*left + f.leftZ1
		f.leftZ1 = f.b1*left - f.a1*leftOut + f.leftZ2
		f.leftZ2 = f.b2*left - f.a2*leftOut
		buf[i] = leftOut

		right := buf[i+1]
		rightOut := f.b0*right + f.rightZ1
		f.rightZ1 = f.b1*right - f.a1*rightOut + f.rightZ2
		f.rightZ2 = f.b2*right - f.a2*rightOut
		buf[i+1] = rightOut
	}
}

type DelayLine struct {
	Feedback float64
	Wet      float64
	Dry      float64

	left  []float64
	right []float64
	index int
	delay int
}

func NewDelayLine() *DelayLine {
	size := int(delayMaxSeconds * SampleRate)
	return &DelayLine{
		left:  make([]float64, size),
		right: make([]float64, size),
		delay: int(0.25 * SampleRate),
		Dry:   1,
	}
}

func (d *DelayLine) SetDelaySeconds(seconds float64) {
	samples := int(seconds * SampleRate)
	if samples < 1 {
		samples = 1
	}
	if samples >= len(d.left) {
		samples = len(d.left) - 1
	}
	d.delay = samples
}

func (d *DelayLine) Reset() {
	for i := range d.left {
		d.left[i] = 0
		d.right[i] = 0
	}
	d.index = 0
}

func (d *DelayLine) ProcessStereo(buf []float64) {
	size := len(d.left)
	if size == 0 {
		return
	}
	for i := 0; i+1 < len(buf); i += 2 {
		readIndex := d.index - d.delay
		if readIndex < 0 {
			readIndex += size
		}
		delayedLeft := d.left[readIndex]
		delayedRight := d.right[readIndex]

		d.left[d.index] = buf[i] + delayedLeft*d.Feedback
		d.right[d.index] = buf[i+1] + delayedRight*d.Feedback

		buf[i] = buf[i]*d.Dry + delayedLeft*d.Wet
		buf[i+1] = buf[i+1]*d.Dry + delayedRight*d.Wet

		d.index++
		if d.index >= size {
			d.index = 0
		}
	}
}

type combFilter struct {
	buffer   []float64
	index    int
	store    float64
	feedback float64
	damping  float64
}

func newCombFilter(size int) *combFilter {
	return &combFilter{buffer: make([]float64, size)}
}

func (c *combFilter) process(input float64) float64 {
	output := c.buffer[c.index]
	c.store = output*(1-c.damping) + c.store*c.damping
	c.buffer[c.index] = input + c.store*c.feedback
	c.index++
	if c.index >= len(c.buffer) {
		c.index = 0
	}
	return output
}

func (c *combFilter) reset() {
	for i := range c.buffer {
		c.buffer[i] = 0
	}
	c.index = 0
	c.store = 0
}

type allpassFilter struct {
	buffer   []float64
	index    int
	feedback float64
}

func newAllpassFilter(size int) *allpassFilter {
	return &allpassFilter{buffer: make([]float64, size), feedback: 0.5}
}

func (a *allpassFilter) process(input float64) float64 {
	buffered := a.buffer[a.index]
	output := -input + buffered
	a.buffer[a.index] = input + buffered*a.feedback
	a.index++
	if a.index >= len(a.buffer) {
		a.index = 0
	}
	return output
}

func (a *allpassFilter) reset() {
	for i := range a.buffer {
		a.buffer[i] = 0
	}
	a.index = 0
}

type Reverb struct {
	leftCombs    [reverbCombCount]*combFilter
	rightCombs   [reverbCombCount]*combFilter
	leftAllpass  [reverbAllpassCount]*allpassFilter
	rightAllpass [reverbAllpassCount]*allpassFilter
	scratch      []float64
}

func NewReverb() *Reverb {
	r := &Reverb{scratch: make([]float64, FrameSize*Channels)}
	for i := 0; i < reverbCombCount; i++ {
		r.leftCombs[i] = newCombFilter(reverbCombLengths[i])
		r.rightCombs[i] = newCombFilter(reverbCombLengths[i] + reverbStereoSpread)
		r.leftCombs[i].feedback = reverbRoomSize
		r.rightCombs[i].feedback = reverbRoomSize
		r.leftCombs[i].damping = reverbDamping
		r.rightCombs[i].damping = reverbDamping
	}
	for i := 0; i < reverbAllpassCount; i++ {
		r.leftAllpass[i] = newAllpassFilter(reverbAllpassLengths[i])
		r.rightAllpass[i] = newAllpassFilter(reverbAllpassLengths[i] + reverbStereoSpread)
	}
	return r
}

func (r *Reverb) Reset() {
	for i := 0; i < reverbCombCount; i++ {
		r.leftCombs[i].reset()
		r.rightCombs[i].reset()
	}
	for i := 0; i < reverbAllpassCount; i++ {
		r.leftAllpass[i].reset()
		r.rightAllpass[i].reset()
	}
}

func (r *Reverb) ProcessStereo(buf []float64, dry, wet float64) {
	if len(r.scratch) < len(buf) {
		r.scratch = make([]float64, len(buf))
	}
	for i := 0; i+1 < len(buf); i += 2 {
		inputLeft := buf[i] * 0.015
		inputRight := buf[i+1] * 0.015

		var accLeft, accRight float64
		for c := 0; c < reverbCombCount; c++ {
			accLeft += r.leftCombs[c].process(inputLeft)
			accRight += r.rightCombs[c].process(inputRight)
		}
		for a := 0; a < reverbAllpassCount; a++ {
			accLeft = r.leftAllpass[a].process(accLeft)
			accRight = r.rightAllpass[a].process(accRight)
		}

		buf[i] = buf[i]*dry + accLeft*wet
		buf[i+1] = buf[i+1]*dry + accRight*wet
	}
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
		if sample > 32767 {
			dst[i] = 32767
		} else if sample < -32768 {
			dst[i] = -32768
		} else {
			dst[i] = int16(sample)
		}
	}
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

func SilenceFloat(buf []float64) {
	for i := range buf {
		buf[i] = 0
	}
}

func SweepFrequency(from, to, progress float64) float64 {
	return from * math.Pow(to/from, ClampUnit(progress))
}

func RampAt(progress, center, width float64) float64 {
	if width <= 0 {
		if progress >= center {
			return 1
		}
		return 0
	}
	return SmoothStep((progress - center + width/2) / width)
}

func BellAt(progress, center, width float64) float64 {
	if width <= 0 {
		return 0
	}
	distance := math.Abs(progress-center) / (width / 2)
	if distance >= 1 {
		return 0
	}
	return math.Cos(distance * math.Pi / 2)
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
