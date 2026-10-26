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

type jogMotion struct {
	spanSec  float64
	riseSec  float64
	peak     float64
	exponent float64
}

type Jogwheel struct {
	jogMotion
	history  []float64
	written  int
	start    float64
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
		jogMotion: jogMotion{
			spanSec:  spanSec,
			riseSec:  math.Min(spinbackRiseSec, spanSec/2),
			peak:     peak,
			exponent: spinbackExponent,
		},
		history: make([]float64, int(travelSec*dsp.SampleRate)*dsp.Channels),
		start:   start,
	}
}

func startVinylStop(beats, end float64, window *Window) *Jogwheel {
	start, spanSec := jogSpan(beats, end, window)
	return &Jogwheel{
		jogMotion: jogMotion{spanSec: spanSec, exponent: vinylStopExponent},
		history:   make([]float64, int((spanSec+jogMarginSec)*dsp.SampleRate)*dsp.Channels),
		start:     start,
	}
}

func (j *Jogwheel) Process(buf []float64, progress, frameStep float64) {
	steps := len(buf) / dsp.Channels
	motion, history, start := j.jogMotion, j.history, j.start
	written, elapsed, position := j.written, j.elapsed, j.position
	isEngaged, isStopped := j.engaged, j.stopped
	var work [dsp.FrameSize * dsp.Channels]float64
	for first := 0; first < steps*dsp.Channels; first += len(work) {
		chunk := buf[first:min(first+len(work), steps*dsp.Channels)]
		count := copy(work[:], chunk)
		for offset := 0; offset < count; offset += dsp.Channels {
			i := (first + offset) / dsp.Channels
			written = rememberSample(history, written, work[offset], work[offset+1]) // #nosec G602
			if !isEngaged {
				if progress+float64(i)/float64(steps)*frameStep < start {
					continue
				}
				isEngaged = true
				position = float64(written - 2)
			}
			speed, hasStopped := motion.speedAt(float64(elapsed) / dsp.SampleRate)
			isStopped = isStopped || hasStopped
			elapsed++
			left, right := readHistory(history, written, position)
			gain := 0.0
			if !isStopped {
				gain = math.Min(1, math.Abs(speed)/jogFadeSpeed)
			}
			work[offset] = left * gain
			work[offset+1] = right * gain // #nosec G602
			position += speed
		}
		copy(chunk, work[:count])
	}
	j.written, j.elapsed, j.position = written, elapsed, position
	j.engaged, j.stopped = isEngaged, isStopped
}

func (m *jogMotion) speedAt(elapsedSec float64) (float64, bool) {
	if m.peak == 0 {
		progress := elapsedSec / m.spanSec
		if progress >= 1 {
			return 0, true
		}
		return math.Pow(1-progress, m.exponent), false
	}
	if elapsedSec < m.riseSec {
		return 1 - (1+m.peak)*elapsedSec/m.riseSec, false
	}
	progress := (elapsedSec - m.riseSec) / (m.spanSec - m.riseSec)
	if progress >= 1 {
		return 0, true
	}
	return -m.peak * math.Pow(1-progress, m.exponent), false
}

func rememberSample(history []float64, written int, left, right float64) int {
	index := (written % (len(history) / dsp.Channels)) * dsp.Channels
	history[index] = left
	history[index+1] = right
	return written + 1
}

func readHistory(history []float64, written int, position float64) (float64, float64) {
	capacity := len(history) / dsp.Channels
	base := math.Floor(position)
	if base < 0 || base < float64(written-capacity) || base+1 > float64(written-1) {
		return 0, 0
	}
	blend := position - base
	first := (int(base) % capacity) * dsp.Channels
	second := ((int(base) + 1) % capacity) * dsp.Channels
	left := history[first] + (history[second]-history[first])*blend
	right := history[first+1] + (history[second+1]-history[first+1])*blend
	return left, right
}
