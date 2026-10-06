package dsp

import "math"

const (
	crushBitsSpan = 10.0
	crushHoldSpan = 7.0
)

type Bitcrusher struct {
	held  [Channels]float64
	count int
}

func CrushQuantum(amount float64) float64 {
	return math.Exp2(crushBitsSpan * amount)
}

func CrushHold(amount float64) int {
	return 1 + int(math.Round(crushHoldSpan*amount))
}

func (b *Bitcrusher) Process(block []float64, from, to float64) {
	steps := len(block) / Channels
	for i := 0; i+1 < len(block); i += Channels {
		amount := from + (to-from)*float64(i/Channels+1)/float64(steps)
		if amount <= 0 {
			b.count = 0
			continue
		}
		if b.count%CrushHold(amount) == 0 {
			quantum := CrushQuantum(amount)
			b.held[0] = math.Round(block[i]/quantum) * quantum
			b.held[1] = math.Round(block[i+1]/quantum) * quantum
		}
		b.count++
		block[i], block[i+1] = b.held[0], b.held[1]
	}
}
