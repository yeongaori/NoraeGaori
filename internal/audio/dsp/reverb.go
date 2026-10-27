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
	reverbCombCount       = 8
	reverbAllpassCount    = 4
)

var (
	reverbCombLengths = [Channels][reverbCombCount]int{
		{1214, 1293, 1390, 1476, 1548, 1623, 1695, 1760},
		{1249, 1327, 1423, 1511, 1583, 1657, 1733, 1801},
	}
	reverbAllpassLengths = [Channels][reverbAllpassCount]int{
		{605, 480, 371, 245},
		{617, 491, 383, 257},
	}
)

type delayRing struct {
	buffer []float64
	index  int
}

func (d *delayRing) read(window []float64) {
	copied := copy(window, d.buffer[d.index:])
	copy(window[copied:], d.buffer)
}

func (d *delayRing) write(window []float64) {
	copied := copy(d.buffer[d.index:], window)
	copy(d.buffer, window[copied:])
	d.index = (d.index + len(window)) % len(d.buffer)
}

type combFilter struct {
	delayRing
	store    float64
	feedback float64
}

type reverbChannel struct {
	combs     [reverbCombCount]combFilter
	allpasses [reverbAllpassCount]delayRing
}

func createReverbChannel(combLengths *[reverbCombCount]int, allpassLengths *[reverbAllpassCount]int) reverbChannel {
	var channel reverbChannel
	for i, length := range combLengths {
		channel.combs[i] = combFilter{
			delayRing: delayRing{buffer: make([]float64, length)},
			feedback:  math.Pow(10, -3*float64(length)/(reverbDecaySeconds*SampleRate)),
		}
	}
	for i, length := range allpassLengths {
		channel.allpasses[i] = delayRing{buffer: make([]float64, length)}
	}
	return channel
}

func (c *reverbChannel) process(sends *[workSamples]float64, steps int) [workSamples]float64 {
	var sums, window [workSamples]float64
	for i := range c.combs {
		comb := &c.combs[i]
		comb.read(window[:steps])
		store, feedback := comb.store, comb.feedback
		for step := 0; step < steps; step++ {
			delayed := window[step]
			store = delayed*(1-reverbDamping) + store*reverbDamping
			window[step] = sends[step] + store*feedback
			sums[step] += delayed
		}
		comb.write(window[:steps])
		comb.store = store
	}
	for i := range c.allpasses {
		allpass := &c.allpasses[i]
		allpass.read(window[:steps])
		for step := 0; step < steps; step++ {
			buffered := window[step]
			window[step] = sums[step] + buffered*reverbAllpassFeedback
			sums[step] = buffered - sums[step]
		}
		allpass.write(window[:steps])
	}
	return sums
}

type Reverb struct {
	predelay delayRing
	lowCut   Filter
	highCut  Filter
	channels [Channels]reverbChannel
}

func PrepareReverb() *Reverb {
	reverb := &Reverb{predelay: delayRing{buffer: make([]float64, int(reverbPredelaySeconds*SampleRate))}}
	for channel := range reverb.channels {
		reverb.channels[channel] = createReverbChannel(&reverbCombLengths[channel], &reverbAllpassLengths[channel])
	}
	reverb.lowCut.SetHighpass(reverbLowCutoff, butterworthQ)
	reverb.highCut.SetLowpass(reverbHighCutoff, reverbHighQ)
	return reverb
}

func (r *Reverb) Render(input, output []float64) {
	for start := 0; start+1 < len(input); start += workLength {
		end := min(start+workLength, len(input))
		r.renderWork(input[start:end], output[start:end])
	}
}

func (r *Reverb) renderWork(input, output []float64) {
	var samples workBuffer
	steps := copy(samples[:], input) / Channels

	var sends, averages [workSamples]float64
	r.predelay.read(sends[:steps])
	for step := 0; step < steps; step++ {
		averages[step] = (samples[step*Channels] + samples[step*Channels+1]) / 2
	}
	r.predelay.write(averages[:steps])

	lowCut, lowState := r.lowCut.filterCoefficients, r.lowCut.states[0]
	for step := 0; step < steps; step++ {
		lowState, sends[step] = lowCut.next(lowState, sends[step])
	}
	r.lowCut.states[0] = lowState

	highCut := r.highCut.filterCoefficients
	for channel := range r.channels {
		sums := r.channels[channel].process(&sends, steps)
		highState := r.highCut.states[channel]
		for step := 0; step < steps; step++ {
			var wet float64
			highState, wet = highCut.next(highState, sums[step])
			samples[step*Channels+channel] = wet * reverbLevel
		}
		r.highCut.states[channel] = highState
	}
	copy(output, samples[:steps*Channels])
}
