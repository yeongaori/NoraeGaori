package dsp

import "math"

const (
	limiterAttackSamples = 48
	limiterReleaseSec    = 0.08
	limiterKnee          = 0.9
	limiterIdleReduction = 1e-4
)

var limiterRelease = math.Exp(-1 / (limiterReleaseSec * SampleRate))

type Limiter struct {
	reduction float64
	needs     []float64
	targets   []float64
	window    []int
}

func (l *Limiter) ProcessStereo(buf []float64, ceiling float64) {
	steps := len(buf) / Channels
	if cap(l.targets) < steps {
		l.needs = make([]float64, steps)
		l.targets = make([]float64, steps)
		l.window = make([]int, steps)
	}
	needs := l.needs[:steps]
	targets := l.targets[:steps]
	window := l.window[:steps]

	for i := range needs {
		peak := math.Max(math.Abs(buf[i*Channels]), math.Abs(buf[i*Channels+1]))
		needs[i] = 1
		if peak > ceiling {
			needs[i] = ceiling / peak
		}
	}

	head, tail := 0, 0
	for i := steps - 1; i >= 0; i-- {
		for tail > head && needs[window[tail-1]] >= needs[i] {
			tail--
		}
		window[tail] = i
		tail++
		for window[head] > i+limiterAttackSamples {
			head++
		}
		targets[i] = needs[window[head]]
		if i+1 < steps {
			targets[i] = math.Min(targets[i], targets[i+1]+1.0/limiterAttackSamples)
		}
	}

	reduction := l.reduction
	for i, target := range targets {
		previous := 1 - reduction
		gain := math.Min(1-reduction*limiterRelease, target)
		gain = math.Max(gain, previous-1.0/limiterAttackSamples)
		reduction = 1 - gain
		if reduction < limiterIdleReduction {
			reduction = 0
		}
		if gain == 1 {
			continue
		}
		buf[i*Channels] *= gain
		buf[i*Channels+1] *= gain
		if gain > target {
			buf[i*Channels] = softClip(buf[i*Channels], ceiling)
			buf[i*Channels+1] = softClip(buf[i*Channels+1], ceiling)
		}
	}
	l.reduction = reduction
}

func (l *Limiter) IsIdle() bool {
	return l.reduction == 0
}

func softClip(sample, ceiling float64) float64 {
	knee := limiterKnee * ceiling
	magnitude := math.Abs(sample)
	if magnitude <= knee {
		return sample
	}
	room := ceiling - knee
	return math.Copysign(knee+room*math.Tanh((magnitude-knee)/room), sample)
}
