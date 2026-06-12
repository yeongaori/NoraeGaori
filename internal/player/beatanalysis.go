package player

import (
	"fmt"
	"math"
)

const (
	beatHop    = 512
	beatWin    = 1024
	beatMinBPM = 70.0
	beatMaxBPM = 180.0
	beatMinSeconds = 8.0
)

type TrackAnalysis struct {
	BPM       float64
	PeriodSec float64
	FirstBeat float64
	Duration  float64
}

func onsetEnvelope(samples []float32, sampleRate float64) ([]float64, float64) {
	nFrames := 0
	if len(samples) > beatWin {
		nFrames = (len(samples) - beatWin) / beatHop
	}

	rms := make([]float64, nFrames)
	for f := 0; f < nFrames; f++ {
		start := f * beatHop
		var sum float64
		for i := 0; i < beatWin; i++ {
			s := float64(samples[start+i])
			sum += s * s
		}
		rms[f] = math.Sqrt(sum / beatWin)
	}

	novelty := make([]float64, nFrames)
	for f := 1; f < nFrames; f++ {
		d := math.Log1p(rms[f]) - math.Log1p(rms[f-1])
		if d > 0 {
			novelty[f] = d
		}
	}

	const w = 16
	smoothed := make([]float64, nFrames)
	for f := 0; f < nFrames; f++ {
		var acc float64
		var n int
		for k := -w; k <= w; k++ {
			idx := f + k
			if idx >= 0 && idx < nFrames {
				acc += novelty[idx]
				n++
			}
		}
		local := 0.0
		if n > 0 {
			local = acc / float64(n)
		}
		v := novelty[f] - local
		if v > 0 {
			smoothed[f] = v
		}
	}

	return smoothed, sampleRate / beatHop
}

func estimateTempo(novelty []float64, frameRate float64) (float64, float64) {
	minLag := int(math.Floor((60 * frameRate) / beatMaxBPM))
	maxLag := int(math.Ceil((60 * frameRate) / beatMinBPM))
	if minLag < 1 {
		minLag = 1
	}

	scores := make([]float64, maxLag+1)
	bestLag := minLag
	bestScore := math.Inf(-1)
	for lag := minLag; lag <= maxLag; lag++ {
		var acc float64
		for i := lag; i < len(novelty); i++ {
			acc += novelty[i] * novelty[i-lag]
		}
		score := acc / float64(lag)
		scores[lag] = score
		if score > bestScore {
			bestScore = score
			bestLag = lag
		}
	}

	periodFrames := float64(bestLag)
	if bestLag > minLag && bestLag < maxLag {
		yL := scores[bestLag-1]
		yC := scores[bestLag]
		yR := scores[bestLag+1]
		denom := yL - 2*yC + yR
		if denom != 0 {
			delta := (0.5 * (yL - yR)) / denom
			periodFrames = float64(bestLag) + delta
		}
	}

	bpm := (60 * frameRate) / periodFrames
	return bpm, periodFrames
}

func estimateBeatGrid(novelty []float64, frameRate, periodFrames float64) (float64, float64) {
	p := periodFrames
	bestOffset := 0
	bestScore := math.Inf(-1)
	for offset := 0; float64(offset) < p; offset++ {
		var acc float64
		for t := float64(offset); t < float64(len(novelty)); t += p {
			idx := int(math.Round(t))
			if idx >= 0 && idx < len(novelty) {
				acc += novelty[idx]
			}
		}
		if acc > bestScore {
			bestScore = acc
			bestOffset = offset
		}
	}

	periodSec := p / frameRate
	firstBeat := float64(bestOffset) / frameRate
	return firstBeat, periodSec
}

func analyzeTrackSamples(samples []float32, sampleRate float64) (*TrackAnalysis, error) {
	if sampleRate <= 0 {
		return nil, fmt.Errorf("invalid sample rate")
	}
	duration := float64(len(samples)) / sampleRate
	if duration < beatMinSeconds {
		return nil, fmt.Errorf("track too short for analysis: %.2fs", duration)
	}

	novelty, frameRate := onsetEnvelope(samples, sampleRate)
	if len(novelty) == 0 {
		return nil, fmt.Errorf("empty onset envelope")
	}

	var noveltySum float64
	for _, v := range novelty {
		noveltySum += v
	}
	if noveltySum <= 0 {
		return nil, fmt.Errorf("flat onset envelope")
	}

	bpm, periodFrames := estimateTempo(novelty, frameRate)
	if periodFrames <= 0 || math.IsNaN(periodFrames) || math.IsInf(periodFrames, 0) {
		return nil, fmt.Errorf("degenerate tempo estimate")
	}

	firstBeat, periodSec := estimateBeatGrid(novelty, frameRate, periodFrames)
	if periodSec < 60.0/beatMaxBPM || periodSec > 60.0/beatMinBPM {
		return nil, fmt.Errorf("beat period out of range: %.3fs", periodSec)
	}

	return &TrackAnalysis{
		BPM:       bpm,
		PeriodSec: periodSec,
		FirstBeat: firstBeat,
		Duration:  duration,
	}, nil
}

func snapToBeat(t, firstBeat, periodSec float64) float64 {
	if periodSec <= 0 {
		return t
	}
	k := math.Round((t - firstBeat) / periodSec)
	return firstBeat + k*periodSec
}

func qsinIn(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	return math.Sin(p * math.Pi / 2)
}

func qsinOut(p float64) float64 {
	if p <= 0 {
		return 1
	}
	if p >= 1 {
		return 0
	}
	return math.Cos(p * math.Pi / 2)
}
