package dsp

import "math"

const (
	phaserStages   = 6
	phaserLowHz    = 300.0
	phaserHighHz   = 3000.0
	phaserFeedback = 0.6
	phaserRatio    = phaserHighHz / phaserLowHz
)

var (
	phaserLogRatio                   = math.Log(phaserRatio)
	phaserRatioMantissa, phaserScale = splitPhaserRatio()
)

func splitPhaserRatio() (float64, float64) {
	mantissa, exponent := math.Frexp(phaserRatio)
	return mantissa, math.Ldexp(1, exponent)
}

func raisePhaserRatio(exponent float64) float64 {
	switch {
	case exponent > 0 && exponent < 0.5:
		return math.Exp(exponent * phaserLogRatio)
	case exponent > 0.5 && exponent < 1:
		return math.Exp((exponent-1)*phaserLogRatio) * phaserRatioMantissa * phaserScale
	}
	return math.Pow(phaserRatio, exponent)
}

type allpassStage struct {
	input  float64
	output float64
}

type phaserChannel struct {
	stages   [phaserStages]allpassStage
	feedback float64
}

type Phaser struct {
	step  float64
	phase float64
	left  phaserChannel
	right phaserChannel
}

func PreparePhaser(rateHz float64) *Phaser {
	return &Phaser{step: 2 * math.Pi * rateHz / SampleRate}
}

func PhaserFrequency(phase float64) float64 {
	return phaserLowHz * raisePhaserRatio((1+math.Sin(phase))/2)
}

func phaserCoefficient(phase float64) float64 {
	tangent := math.Tan(math.Pi * PhaserFrequency(phase) / SampleRate)
	return (tangent - 1) / (tangent + 1)
}

func (p *Phaser) Process(block []float64, from, to float64) {
	steps := len(block) / Channels
	left, right, phase, step := p.left, p.right, p.phase, p.step
	var work workBuffer
	for start := 0; start+1 < len(block); start += workLength {
		chunk := block[start:min(start+workLength, len(block))]
		end := copy(work[:], chunk) &^ 1
		for i := 0; i < end; i += Channels {
			mix := from + (to-from)*float64((start+i)/Channels+1)/float64(steps)
			var wetLeft, wetRight float64
			left, wetLeft = left.run(work[i], phaserCoefficient(phase))
			right, wetRight = right.run(work[i+1], phaserCoefficient(phase+math.Pi/2)) // #nosec G602
			work[i] += mix * (wetLeft - work[i])
			work[i+1] += mix * (wetRight - work[i+1]) // #nosec G602
			phase += step
			if phase >= 2*math.Pi || phase <= -2*math.Pi {
				phase = math.Mod(phase, 2*math.Pi)
			}
		}
		copy(chunk, work[:end])
	}
	p.left, p.right, p.phase = left, right, phase
}

func (c phaserChannel) run(input, coefficient float64) (phaserChannel, float64) {
	signal := input + c.feedback*phaserFeedback
	for index := range c.stages {
		output := coefficient*signal + c.stages[index].input - coefficient*c.stages[index].output
		c.stages[index] = allpassStage{input: signal, output: output}
		signal = output
	}
	c.feedback = signal
	return c, signal
}
