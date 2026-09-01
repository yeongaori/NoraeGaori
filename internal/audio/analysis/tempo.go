package analysis

import "math"

const (
	KeyConfidenceFloor = 0.04
	TempoFoldTolerance = 0.12
)

func TempoDeltaFactor(bpmA, bpmB float64) (float64, float64) {
	if bpmA <= 0 || bpmB <= 0 {
		return 0, 1
	}
	best := math.Abs(bpmB-bpmA) / bpmA
	bestFactor := 1.0
	for _, factor := range []float64{0.5, 2} {
		delta := math.Abs(bpmB*factor-bpmA) / bpmA
		if delta < best && delta <= TempoFoldTolerance {
			best = delta
			bestFactor = factor
		}
	}
	return best, bestFactor
}

func TempoDelta(bpmA, bpmB float64) float64 {
	delta, _ := TempoDeltaFactor(bpmA, bpmB)
	return delta
}

func SignedTempoDelta(bpmA, bpmB float64) float64 {
	if bpmA <= 0 || bpmB <= 0 {
		return 0
	}
	best := (bpmB - bpmA) / bpmA
	for _, factor := range []float64{0.5, 2} {
		if delta := (bpmB*factor - bpmA) / bpmA; math.Abs(delta) < math.Abs(best) {
			best = delta
		}
	}
	return best
}
