package analysis

import "math"

const (
	barSearchFraction = 0.25
	barTrackFraction  = 0.125
	barSearchStep     = 0.25
	minTrackedBars    = 4
)

func barOffsets(novelty []float64, frameRate, periodFrames, firstDownbeatFrames float64) []float64 {
	reach := periodFrames * barSearchFraction
	var offsets []float64
	for start := firstDownbeatFrames; barFits(novelty, start, periodFrames); start += BarBeats * periodFrames {
		offsets = append(offsets, bestBarShift(novelty, start, periodFrames, -reach, reach)/frameRate)
	}
	return offsets
}

func refineGrid(novelty []float64, periodFrames, firstDownbeatFrames float64) (float64, float64) {
	reach := periodFrames * barTrackFraction
	var bars, shifts []float64
	shift := 0.0
	for bar := 0; ; bar++ {
		start := firstDownbeatFrames + float64(bar)*BarBeats*periodFrames
		if !barFits(novelty, start, periodFrames) {
			break
		}
		shift = bestBarShift(novelty, start, periodFrames, shift-reach, shift+reach)
		bars = append(bars, float64(bar))
		shifts = append(shifts, shift)
	}
	if len(bars) < minTrackedBars {
		return periodFrames, firstDownbeatFrames
	}
	slope, intercept := fitLine(bars, shifts)
	return periodFrames + slope/BarBeats, firstDownbeatFrames + intercept
}

func barFits(novelty []float64, start, periodFrames float64) bool {
	return start >= 0 && start+(BarBeats+barSearchFraction)*periodFrames < float64(len(novelty)-1)
}

func bestBarShift(novelty []float64, start, periodFrames, low, high float64) float64 {
	bestShift, bestScore := 0.0, -1.0
	for shift := low; shift <= high; shift += barSearchStep {
		score := 0.0
		for beat := 0; beat < BarBeats; beat++ {
			score += noveltyAt(novelty, start+shift+float64(beat)*periodFrames)
		}
		if score > bestScore {
			bestShift, bestScore = shift, score
		}
	}
	return bestShift
}

func fitLine(xs, ys []float64) (float64, float64) {
	var meanX, meanY float64
	for i := range xs {
		meanX += xs[i]
		meanY += ys[i]
	}
	meanX /= float64(len(xs))
	meanY /= float64(len(ys))
	var covariance, spread float64
	for i := range xs {
		covariance += (xs[i] - meanX) * (ys[i] - meanY)
		spread += (xs[i] - meanX) * (xs[i] - meanX)
	}
	if spread == 0 {
		return 0, meanY
	}
	slope := covariance / spread
	return slope, meanY - slope*meanX
}

func splitDownbeat(firstDownbeatFrames, periodFrames float64, phase int) (float64, int) {
	firstBeatFrames := firstDownbeatFrames - float64(phase)*periodFrames
	for firstBeatFrames < 0 {
		firstBeatFrames += periodFrames
		phase = (phase + BarBeats - 1) % BarBeats
	}
	for firstBeatFrames >= periodFrames {
		firstBeatFrames -= periodFrames
		phase = (phase + 1) % BarBeats
	}
	return firstBeatFrames, phase
}

func noveltyAt(novelty []float64, position float64) float64 {
	index := int(math.Floor(position))
	if index < 0 || index+1 >= len(novelty) {
		return 0
	}
	blend := position - float64(index)
	return novelty[index]*(1-blend) + novelty[index+1]*blend
}

func (a *TrackAnalysis) FirstDownbeat() float64 {
	return a.Offset + a.FirstBeat + float64(a.DownbeatPhase)*a.PeriodSec
}

func (a *TrackAnalysis) BarLength() float64 {
	return BarBeats * a.PeriodSec
}

func (a *TrackAnalysis) OffsetAt(position float64) (float64, bool) {
	bar := (position - a.FirstDownbeat()) / a.BarLength()
	index := int(math.Floor(bar))
	if index < 0 || index >= len(a.BarOffsets) {
		return 0, false
	}
	if index+1 >= len(a.BarOffsets) {
		return a.BarOffsets[index], bar == float64(index)
	}
	blend := bar - float64(index)
	return a.BarOffsets[index]*(1-blend) + a.BarOffsets[index+1]*blend, true
}

func (a *TrackAnalysis) BarLengthVariance(start, end float64) (float64, bool) {
	first := int(math.Ceil((start-a.FirstDownbeat())/a.BarLength() - 1e-6))
	last := int(math.Floor((end-a.FirstDownbeat())/a.BarLength() + 1e-6))
	if first < 0 || last >= len(a.BarOffsets) || last-first < 1 {
		return 0, false
	}
	lengths := make([]float64, 0, last-first)
	var mean float64
	for bar := first; bar < last; bar++ {
		length := a.BarLength() + a.BarOffsets[bar+1] - a.BarOffsets[bar]
		lengths = append(lengths, length)
		mean += length
	}
	mean /= float64(len(lengths))
	var variance float64
	for _, length := range lengths {
		variance += (length - mean) * (length - mean)
	}
	return variance / float64(len(lengths)), true
}
