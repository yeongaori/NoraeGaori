package analysis

import (
	"fmt"
	"math"

	"noraegaori/internal/logger"
)

const (
	BeatHop    = 512
	beatWin    = 1024
	MinBPM     = 70.0
	MaxBPM     = 210.0
	MinSeconds = 8.0
	SampleRate = 24000.0
)

type TrackAnalysis struct {
	BPM           float64
	PeriodSec     float64
	FirstBeat     float64
	Duration      float64
	Tonic         int
	Minor         bool
	KeyConfidence float64
	DownbeatPhase int
}

func onsetEnvelope(samples []float32, sampleRate float64) ([]float64, float64) {
	nFrames := 0
	if len(samples) > beatWin {
		nFrames = (len(samples) - beatWin) / BeatHop
	}

	rms := make([]float64, nFrames)
	for f := 0; f < nFrames; f++ {
		start := f * BeatHop
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

	return smoothed, sampleRate / BeatHop
}

func TempoLagRange(frameRate float64) (int, int) {
	minLag := int(math.Ceil((60 * frameRate) / MaxBPM))
	maxLag := int(math.Floor((60 * frameRate) / MinBPM))
	if minLag < 1 {
		minLag = 1
	}
	if maxLag < minLag {
		maxLag = minLag
	}
	return minLag, maxLag
}

func estimateTempo(novelty []float64, frameRate float64) (float64, float64) {
	minLag, maxLag := TempoLagRange(frameRate)

	scanMin := minLag - 1
	if scanMin < 1 {
		scanMin = 1
	}
	scanMax := maxLag + 1

	scores := make([]float64, scanMax+1)
	bestLag := minLag
	bestScore := math.Inf(-1)
	for lag := scanMin; lag <= scanMax; lag++ {
		var acc float64
		for i := lag; i < len(novelty); i++ {
			acc += novelty[i] * novelty[i-lag]
		}
		score := acc / float64(lag)
		scores[lag] = score
		if lag >= minLag && lag <= maxLag && score > bestScore {
			bestScore = score
			bestLag = lag
		}
	}

	periodFrames := float64(bestLag)
	escaped := ""
	switch {
	case bestLag == minLag && scanMin < minLag && scores[scanMin] > scores[bestLag]:
		periodFrames = float64(scanMin)
		escaped = "faster-than-band"
	case bestLag == maxLag && scores[scanMax] > scores[bestLag]:
		periodFrames = float64(scanMax)
		escaped = "slower-than-band"
	default:
		yL := scores[bestLag-1]
		yC := scores[bestLag]
		yR := scores[bestLag+1]
		denom := yL - 2*yC + yR
		if denom < 0 {
			delta := (0.5 * (yL - yR)) / denom
			if math.Abs(delta) < 1 {
				periodFrames = float64(bestLag) + delta
			}
		}
	}

	bpm := (60 * frameRate) / periodFrames
	boundary := "interior"
	if bestLag == minLag {
		boundary = "at-min"
	} else if bestLag == maxLag {
		boundary = "at-max"
	}
	if escaped != "" {
		boundary = escaped
	}
	logger.Debugf("bpm=%.2f period=%.4fs bestLag=%d lag=%.3f range=%d-%d %s frameRate=%.4f",
		bpm, periodFrames/frameRate, bestLag, periodFrames, minLag, maxLag, boundary, frameRate)
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

func AnalyzeTrackSamples(samples []float32, sampleRate float64) (*TrackAnalysis, error) {
	if sampleRate <= 0 {
		return nil, fmt.Errorf("invalid sample rate")
	}
	duration := float64(len(samples)) / sampleRate
	if duration < MinSeconds {
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
	if periodSec < 60.0/MaxBPM || periodSec > 60.0/MinBPM {
		return nil, fmt.Errorf("beat period out of range: %.3fs", periodSec)
	}

	downbeatPhase := estimateDownbeatPhase(novelty, periodFrames, firstBeat*frameRate)
	tonic, minor, keyConfidence := AnalyzeKey(samples, sampleRate)

	return &TrackAnalysis{
		BPM:           bpm,
		PeriodSec:     periodSec,
		FirstBeat:     firstBeat,
		Duration:      duration,
		Tonic:         tonic,
		Minor:         minor,
		KeyConfidence: keyConfidence,
		DownbeatPhase: downbeatPhase,
	}, nil
}
