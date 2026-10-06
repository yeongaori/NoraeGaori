package dsp

import "math"

const (
	phaserStages   = 6
	phaserLowHz    = 300.0
	phaserHighHz   = 3000.0
	phaserFeedback = 0.6
)

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
	return phaserLowHz * math.Pow(phaserHighHz/phaserLowHz, (1+math.Sin(phase))/2)
}

func phaserCoefficient(phase float64) float64 {
	tangent := math.Tan(math.Pi * PhaserFrequency(phase) / SampleRate)
	return (tangent - 1) / (tangent + 1)
}

func (p *Phaser) Process(block []float64, from, to float64) {
	steps := len(block) / Channels
	for i := 0; i+1 < len(block); i += Channels {
		mix := from + (to-from)*float64(i/Channels+1)/float64(steps)
		wetLeft := p.left.run(block[i], phaserCoefficient(p.phase))
		wetRight := p.right.run(block[i+1], phaserCoefficient(p.phase+math.Pi/2))
		block[i] += mix * (wetLeft - block[i])
		block[i+1] += mix * (wetRight - block[i+1])
		p.phase = math.Mod(p.phase+p.step, 2*math.Pi)
	}
}

func (c *phaserChannel) run(input, coefficient float64) float64 {
	signal := input + c.feedback*phaserFeedback
	for index := range c.stages {
		stage := &c.stages[index]
		output := coefficient*signal + stage.input - coefficient*stage.output
		stage.input, stage.output = signal, output
		signal = output
	}
	c.feedback = signal
	return signal
}
