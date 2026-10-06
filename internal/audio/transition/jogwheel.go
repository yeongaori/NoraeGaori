package transition

import (
	"math"

	"noraegaori/internal/audio/dsp"
)

const (
	spinbackExponent  = 1.55
	vinylStopExponent = 2.06
	spinbackRiseSec   = 0.1
	jogFadeSpeed      = 0.25
	jogMarginSec      = 1.0
)

var spinbackPeaks = map[float64]float64{1: 14.7, 2: 11.5, 4: 9.4}

type Jogwheel struct {
	history  []float64
	written  int
	start    float64
	spanSec  float64
	riseSec  float64
	peak     float64
	exponent float64
	engaged  bool
	stopped  bool
	elapsed  int
	position float64
}

func jogSpan(beats, end float64, window *Window) (float64, float64) {
	overlapSec := float64(window.Frames) / dsp.FramesPerSecond
	spanSec := math.Min(beats*beatPeriod(window.PeriodSec), overlapSec*end)
	start := 0.0
	if overlapSec > 0 {
		start = end - spanSec/overlapSec
	}
	return start, spanSec
}

func startSpinback(beats, end float64, window *Window) *Jogwheel {
	start, spanSec := jogSpan(beats, end, window)
	peak := spinbackPeaks[beats]
	travelSec := peak*spanSec/(1+spinbackExponent) + jogMarginSec
	return &Jogwheel{
		history:  make([]float64, int(travelSec*dsp.SampleRate)*dsp.Channels),
		start:    start,
		spanSec:  spanSec,
		riseSec:  math.Min(spinbackRiseSec, spanSec/2),
		peak:     peak,
		exponent: spinbackExponent,
	}
}

func startVinylStop(beats, end float64, window *Window) *Jogwheel {
	start, spanSec := jogSpan(beats, end, window)
	return &Jogwheel{
		history:  make([]float64, int((spanSec+jogMarginSec)*dsp.SampleRate)*dsp.Channels),
		start:    start,
		spanSec:  spanSec,
		exponent: vinylStopExponent,
	}
}

func (j *Jogwheel) Process(buf []float64, progress, frameStep float64) {
	steps := len(buf) / dsp.Channels
	for i := 0; i < steps; i++ {
		j.remember(buf[i*dsp.Channels], buf[i*dsp.Channels+1])
		if !j.engaged {
			if progress+float64(i)/float64(steps)*frameStep < j.start {
				continue
			}
			j.engaged = true
			j.position = float64(j.written - 2)
		}
		speed := j.speedAt(float64(j.elapsed) / dsp.SampleRate)
		j.elapsed++
		left, right := j.read(j.position)
		gain := 0.0
		if !j.stopped {
			gain = math.Min(1, math.Abs(speed)/jogFadeSpeed)
		}
		buf[i*dsp.Channels] = left * gain
		buf[i*dsp.Channels+1] = right * gain
		j.position += speed
	}
}

func (j *Jogwheel) speedAt(elapsedSec float64) float64 {
	if j.peak == 0 {
		progress := elapsedSec / j.spanSec
		if progress >= 1 {
			j.stopped = true
			return 0
		}
		return math.Pow(1-progress, j.exponent)
	}
	if elapsedSec < j.riseSec {
		return 1 - (1+j.peak)*elapsedSec/j.riseSec
	}
	progress := (elapsedSec - j.riseSec) / (j.spanSec - j.riseSec)
	if progress >= 1 {
		j.stopped = true
		return 0
	}
	return -j.peak * math.Pow(1-progress, j.exponent)
}

func (j *Jogwheel) remember(left, right float64) {
	index := (j.written % (len(j.history) / dsp.Channels)) * dsp.Channels
	j.history[index] = left
	j.history[index+1] = right
	j.written++
}

func (j *Jogwheel) read(position float64) (float64, float64) {
	capacity := len(j.history) / dsp.Channels
	base := math.Floor(position)
	if base < 0 || base < float64(j.written-capacity) || base+1 > float64(j.written-1) {
		return 0, 0
	}
	blend := position - base
	first := (int(base) % capacity) * dsp.Channels
	second := ((int(base) + 1) % capacity) * dsp.Channels
	left := j.history[first] + (j.history[second]-j.history[first])*blend
	right := j.history[first+1] + (j.history[second+1]-j.history[first+1])*blend
	return left, right
}
