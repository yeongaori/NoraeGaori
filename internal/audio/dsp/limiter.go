package dsp

import (
	"math"
	"slices"
)

const (
	limiterAttackSamples = 48
	limiterReleaseSec    = 0.08
)

var limiterRelease = math.Exp(-1 / (limiterReleaseSec * SampleRate))

type Limiter struct {
	reduction float64
	needs     []float64
	targets   []float64
}

func (l *Limiter) ProcessStereo(buf []float64, ceiling float64) {
	steps := len(buf) / Channels
	if cap(l.targets) < steps {
		l.needs = make([]float64, steps)
		l.targets = make([]float64, steps)
	}
	needs := l.needs[:steps]
	targets := l.targets[:steps]

	for i := range needs {
		peak := math.Max(math.Abs(buf[i*Channels]), math.Abs(buf[i*Channels+1]))
		needs[i] = 1
		if peak > ceiling {
			needs[i] = ceiling / peak
		}
	}

	for i := range targets {
		targets[i] = slices.Min(needs[i:min(i+limiterAttackSamples+1, steps)])
	}

	for i := steps - 2; i >= 0; i-- {
		targets[i] = math.Min(targets[i], targets[i+1]+1.0/limiterAttackSamples)
	}

	for i, target := range targets {
		gain := math.Min(1-l.reduction*limiterRelease, target)
		l.reduction = 1 - gain
		if gain == 1 {
			continue
		}
		buf[i*Channels] *= gain
		buf[i*Channels+1] *= gain
	}
}
