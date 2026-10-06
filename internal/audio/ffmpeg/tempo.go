package ffmpeg

import (
	"fmt"
	"math"
	"strings"
)

const (
	tempoStep       = 0.005
	tempoStepSec    = 1.0
	TempoSettleSec  = 0.5
	tempoEpsilon    = 1e-9
	normalizeFilter = "dynaudnorm=framelen=500:gausssize=31:peak=0.95"
)

type Tempo struct {
	Speed   float64
	HoldSec float64
}

func (t Tempo) IsActive() bool {
	return t.Speed > 0 && math.Abs(t.Speed-1) > tempoEpsilon
}

func (t Tempo) steps() []float64 {
	count := int(math.Ceil(math.Abs(1-t.Speed)/tempoStep - tempoEpsilon))
	speeds := make([]float64, 0, count)
	direction := math.Copysign(tempoStep, 1-t.Speed)
	for step := 1; step < count; step++ {
		speeds = append(speeds, t.Speed+direction*float64(step))
	}
	return append(speeds, 1)
}

func (t Tempo) filter() string {
	commands := make([]string, 0)
	sourceTime := t.Speed * t.HoldSec
	for _, speed := range t.steps() {
		commands = append(commands, fmt.Sprintf("%.4f atempo tempo %.4f", sourceTime, speed))
		sourceTime += speed * tempoStepSec
	}
	return fmt.Sprintf("asendcmd=c='%s',atempo=%.4f", strings.Join(commands, ";"), t.Speed)
}

func (t Tempo) Drift() float64 {
	if !t.IsActive() {
		return 0
	}
	drift := (t.Speed - 1) * t.HoldSec
	for _, speed := range t.steps() {
		drift += (speed - 1) * tempoStepSec
	}
	return drift
}

func audioFilters(normalization bool, tempo Tempo) []string {
	var filters []string
	if tempo.IsActive() {
		filters = append(filters, tempo.filter())
	}
	if normalization {
		filters = append(filters, normalizeFilter)
	}
	if len(filters) == 0 {
		return nil
	}
	return []string{"-af", strings.Join(filters, ",")}
}
