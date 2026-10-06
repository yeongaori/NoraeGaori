package dsp

import "math"

const (
	reverbPredelaySeconds = 0.045
	reverbDecaySeconds    = 8.5
	reverbDamping         = 0.02
	reverbAllpassFeedback = 0.5
	reverbLowCutoff       = 250.0
	reverbHighCutoff      = 9000.0
	reverbHighQ           = 0.5
	reverbLevel           = 0.0057
)

var (
	reverbLeftCombs      = []int{1214, 1293, 1390, 1476, 1548, 1623, 1695, 1760}
	reverbRightCombs     = []int{1249, 1327, 1423, 1511, 1583, 1657, 1733, 1801}
	reverbLeftAllpasses  = []int{605, 480, 371, 245}
	reverbRightAllpasses = []int{617, 491, 383, 257}
)

type combFilter struct {
	buffer   []float64
	index    int
	store    float64
	feedback float64
}

func (c *combFilter) process(input float64) float64 {
	output := c.buffer[c.index]
	c.store = output*(1-reverbDamping) + c.store*reverbDamping
	c.buffer[c.index] = input + c.store*c.feedback
	c.index++
	if c.index == len(c.buffer) {
		c.index = 0
	}
	return output
}

type allpassFilter struct {
	buffer []float64
	index  int
}

func (a *allpassFilter) process(input float64) float64 {
	buffered := a.buffer[a.index]
	a.buffer[a.index] = input + buffered*reverbAllpassFeedback
	a.index++
	if a.index == len(a.buffer) {
		a.index = 0
	}
	return buffered - input
}

type reverbChannel struct {
	combs     []combFilter
	allpasses []allpassFilter
}

func buildReverbChannel(combLengths, allpassLengths []int) reverbChannel {
	channel := reverbChannel{
		combs:     make([]combFilter, len(combLengths)),
		allpasses: make([]allpassFilter, len(allpassLengths)),
	}
	for i, length := range combLengths {
		channel.combs[i] = combFilter{
			buffer:   make([]float64, length),
			feedback: math.Pow(10, -3*float64(length)/(reverbDecaySeconds*SampleRate)),
		}
	}
	for i, length := range allpassLengths {
		channel.allpasses[i] = allpassFilter{buffer: make([]float64, length)}
	}
	return channel
}

func (c *reverbChannel) process(input float64) float64 {
	var sum float64
	for i := range c.combs {
		sum += c.combs[i].process(input)
	}
	for i := range c.allpasses {
		sum = c.allpasses[i].process(sum)
	}
	return sum
}

type Reverb struct {
	predelay []float64
	index    int
	lowCut   Biquad
	highCut  Biquad
	left     reverbChannel
	right    reverbChannel
}

func PrepareReverb() *Reverb {
	reverb := &Reverb{
		predelay: make([]float64, int(reverbPredelaySeconds*SampleRate)),
		left:     buildReverbChannel(reverbLeftCombs, reverbLeftAllpasses),
		right:    buildReverbChannel(reverbRightCombs, reverbRightAllpasses),
	}
	reverb.lowCut.SetHighpass(reverbLowCutoff, butterworthQ)
	reverb.highCut.SetLowpass(reverbHighCutoff, reverbHighQ)
	return reverb
}

func (r *Reverb) Render(input, output []float64) {
	for i := 0; i+1 < len(input); i += Channels {
		delayed := r.predelay[r.index]
		r.predelay[r.index] = (input[i] + input[i+1]) / 2
		r.index++
		if r.index == len(r.predelay) {
			r.index = 0
		}

		send := r.lowCut.stepLeft(delayed)
		output[i] = r.highCut.stepLeft(r.left.process(send)) * reverbLevel
		output[i+1] = r.highCut.stepRight(r.right.process(send)) * reverbLevel
	}
}
