package analysis

import (
	"fmt"
	"math"

	"noraegaori/internal/logger"
)

const (
	keyDecimation    = 4
	keyWindowSamples = 4096
	keyLowestPitchHz = 65.406
	keySemitoneCount = 48
	keyMinWindows    = 2
	BarBeats         = 4
	keyContrastFloor = 0.12
)

var (
	keyPitchNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

	keyMajorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	keyMinorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

func decimateMono(samples []float32) []float32 {
	if len(samples) < keyDecimation {
		return nil
	}
	out := make([]float32, len(samples)/keyDecimation)
	var accumulator float64
	for i := range samples {
		accumulator += float64(samples[i])
		if i >= keyDecimation {
			accumulator -= float64(samples[i-keyDecimation])
		}
		if i%keyDecimation == 0 && i/keyDecimation < len(out) {
			out[i/keyDecimation] = float32(accumulator / float64(keyDecimation))
		}
	}
	return out
}

func hannWindow(size int) []float64 {
	window := make([]float64, size)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(size-1))
	}
	return window
}

func goertzelMagnitude(samples []float32, window []float64, sampleRate, freq float64) float64 {
	n := len(samples)
	if n == 0 {
		return 0
	}
	omega := 2 * math.Pi * freq / sampleRate
	coefficient := 2 * math.Cos(omega)

	var previous, previousTwo float64
	for i := 0; i < n; i++ {
		value := float64(samples[i]) * window[i]
		current := value + coefficient*previous - previousTwo
		previousTwo = previous
		previous = current
	}

	power := previous*previous + previousTwo*previousTwo - coefficient*previous*previousTwo
	if power <= 0 {
		return 0
	}
	return math.Sqrt(power)
}

func Chromagram(samples []float32, sampleRate float64) ([12]float64, int) {
	var chroma [12]float64

	decimated := decimateMono(samples)
	rate := sampleRate / keyDecimation
	if len(decimated) < keyWindowSamples {
		return chroma, 0
	}

	frequencies := make([]float64, keySemitoneCount)
	for i := range frequencies {
		frequencies[i] = keyLowestPitchHz * math.Pow(2, float64(i)/12)
	}

	window := hannWindow(keyWindowSamples)
	windowCount := 0

	for start := 0; start+keyWindowSamples <= len(decimated); start += keyWindowSamples {
		segment := decimated[start : start+keyWindowSamples]

		var magnitudes [12]float64
		var total float64
		for i, freq := range frequencies {
			if freq >= rate/2 {
				continue
			}
			magnitude := goertzelMagnitude(segment, window, rate, freq)
			magnitudes[i%12] += magnitude
			total += magnitude
		}

		if total <= 0 {
			continue
		}
		for i := range magnitudes {
			chroma[i] += magnitudes[i] / total
		}
		windowCount++
	}

	if windowCount == 0 {
		return chroma, 0
	}
	for i := range chroma {
		chroma[i] /= float64(windowCount)
	}
	return chroma, windowCount
}

func correlation(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var meanA, meanB float64
	for i := range a {
		meanA += a[i]
		meanB += b[i]
	}
	meanA /= float64(len(a))
	meanB /= float64(len(b))

	var covariance, varianceA, varianceB float64
	for i := range a {
		da := a[i] - meanA
		db := b[i] - meanB
		covariance += da * db
		varianceA += da * da
		varianceB += db * db
	}
	if varianceA <= 0 || varianceB <= 0 {
		return 0
	}
	return covariance / math.Sqrt(varianceA*varianceB)
}

func estimateKey(chroma [12]float64) (int, bool, float64) {
	rotated := make([]float64, 12)
	major := keyMajorProfile[:]
	minor := keyMinorProfile[:]

	bestScore := math.Inf(-1)
	secondScore := math.Inf(-1)
	bestTonic := 0
	bestMinor := false

	for tonic := 0; tonic < 12; tonic++ {
		for i := 0; i < 12; i++ {
			rotated[i] = chroma[(tonic+i)%12]
		}

		majorScore := correlation(rotated, major)
		minorScore := correlation(rotated, minor)

		for _, candidate := range [2]struct {
			score float64
			minor bool
		}{{majorScore, false}, {minorScore, true}} {
			if candidate.score > bestScore {
				secondScore = bestScore
				bestScore = candidate.score
				bestTonic = tonic
				bestMinor = candidate.minor
			} else if candidate.score > secondScore {
				secondScore = candidate.score
			}
		}
	}

	if math.IsInf(bestScore, -1) {
		return 0, false, 0
	}
	confidence := bestScore
	if !math.IsInf(secondScore, -1) {
		confidence = bestScore - secondScore
	}
	if confidence < 0 {
		confidence = 0
	}
	return bestTonic, bestMinor, confidence
}

func ChromaContrast(chroma [12]float64) float64 {
	var total float64
	for _, v := range chroma {
		total += v
	}
	if total <= 0 {
		return 0
	}
	mean := total / 12

	var variance float64
	for _, v := range chroma {
		delta := v - mean
		variance += delta * delta
	}
	return math.Sqrt(variance/12) / mean
}

func AnalyzeKey(samples []float32, sampleRate float64) (int, bool, float64) {
	chroma, windows := Chromagram(samples, sampleRate)
	if windows < keyMinWindows {
		logger.Debugf("rejected: only %d chroma windows, need %d", windows, keyMinWindows)
		return 0, false, 0
	}

	contrast := ChromaContrast(chroma)
	if contrast <= 0 {
		logger.Debugf("rejected: flat chromagram across %d windows", windows)
		return 0, false, 0
	}

	tonic, minor, gap := estimateKey(chroma)
	if contrast < keyContrastFloor {
		logger.Debugf("%s verdict=below-contrast gap=%.4f contrast=%.4f contrastFloor=%.4f windows=%d",
			CamelotCode(tonic, minor), gap, contrast, keyContrastFloor, windows)
		return 0, false, 0
	}

	verdict := "accepted"
	if gap < KeyConfidenceFloor {
		verdict = "below-gap"
	}
	logger.Debugf("%s verdict=%s gap=%.4f gapFloor=%.4f contrast=%.4f contrastFloor=%.4f windows=%d",
		CamelotCode(tonic, minor), verdict, gap, KeyConfidenceFloor, contrast, keyContrastFloor, windows)
	return tonic, minor, gap
}

func camelotPosition(tonic int, minor bool) int {
	pitch := tonic
	if minor {
		pitch = (tonic + 3) % 12
	}
	number := (pitch*7)%12 + 8
	if number > 12 {
		number -= 12
	}
	return number
}

func CamelotCode(tonic int, minor bool) string {
	letter := "B"
	if minor {
		letter = "A"
	}
	return fmt.Sprintf("%d%s", camelotPosition(tonic, minor), letter)
}

func KeyName(tonic int, minor bool) string {
	quality := "major"
	if minor {
		quality = "minor"
	}
	return fmt.Sprintf("%s %s", keyPitchNames[tonic%12], quality)
}

func CamelotDistance(a, b *TrackAnalysis) int {
	if a == nil || b == nil {
		return -1
	}
	if a.KeyConfidence < KeyConfidenceFloor || b.KeyConfidence < KeyConfidenceFloor {
		return -1
	}

	positionA := camelotPosition(a.Tonic, a.Minor)
	positionB := camelotPosition(b.Tonic, b.Minor)

	diff := positionA - positionB
	if diff < 0 {
		diff = -diff
	}
	if diff > 6 {
		diff = 12 - diff
	}

	if a.Minor != b.Minor {
		if diff == 0 {
			return 1
		}
		return diff + 1
	}
	return diff
}

func estimateDownbeatPhase(novelty []float64, periodFrames, firstBeatFrames float64) int {
	if periodFrames <= 0 || len(novelty) == 0 {
		return 0
	}

	bestPhase := 0
	bestScore := math.Inf(-1)
	for phase := 0; phase < BarBeats; phase++ {
		var accumulator float64
		counted := 0
		for beat := phase; ; beat += BarBeats {
			position := firstBeatFrames + float64(beat)*periodFrames
			index := int(math.Round(position))
			if index >= len(novelty) {
				break
			}
			if index >= 0 {
				accumulator += novelty[index]
				counted++
			}
		}
		if counted == 0 {
			continue
		}
		score := accumulator / float64(counted)
		if score > bestScore {
			bestScore = score
			bestPhase = phase
		}
	}
	return bestPhase
}
