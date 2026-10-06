package dsp

import "math"

const (
	noiseRiserQ         = 5.0
	noiseRiserLevel     = 1640.0
	noiseRiserBaseFreq  = 393.0
	noiseRiserSlope     = 18.1
	noiseRiserPivot     = 0.608
	noiseRiserOpenWidth = 0.12
	noiseSeed           = 0x9E3779B97F4A7C15
)

type NoiseRiser struct {
	state  uint64
	band   Biquad
	level  float64
	primed bool
}

func NoiseRiserFrequency(colour float64) float64 {
	return noiseRiserBaseFreq * math.Pow(2, noiseRiserSlope*(colour-noiseRiserPivot))
}

func (n *NoiseRiser) Render(output []float64, colour float64) {
	if !n.primed {
		n.state = noiseSeed
		n.primed = true
	}
	frequency := clampFrequency(NoiseRiserFrequency(colour))
	n.band.SetBandpass(frequency, noiseRiserQ)

	bandwidth := math.Pi / 2 * frequency / noiseRiserQ
	scale := noiseRiserLevel * math.Sqrt(3) / math.Sqrt(bandwidth/(SampleRate/2))
	target := scale * SmoothStep((colour-KnobCenter)/noiseRiserOpenWidth)

	steps := len(output) / Channels
	for i := 0; i+1 < len(output); i += Channels {
		gain := n.level + (target-n.level)*float64(i/Channels+1)/float64(steps)
		output[i] = n.band.stepLeft(n.next()) * gain
		output[i+1] = n.band.stepRight(n.next()) * gain
	}
	n.level = target
}

func (n *NoiseRiser) next() float64 {
	n.state ^= n.state >> 12
	n.state ^= n.state << 25
	n.state ^= n.state >> 27
	return float64(int64(n.state*0x2545F4914F6CDD1D)) / math.MaxInt64
}
