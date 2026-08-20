//go:build automixcheck

package player

import (
	"fmt"
	"math"

	"noraegaori/internal/audio/dsp"
)

type CheckResult struct {
	Name   string
	Passed bool
	Detail string
}

type checkCollector struct {
	results []CheckResult
}

func (c *checkCollector) add(name string, passed bool, format string, args ...interface{}) {
	c.results = append(c.results, CheckResult{
		Name:   name,
		Passed: passed,
		Detail: fmt.Sprintf(format, args...),
	})
}

type toneGenerator struct {
	phase     float64
	frequency float64
	amplitude float64
}

func (t *toneGenerator) fill(frame []int16) {
	step := 2 * math.Pi * t.frequency / dsp.SampleRate
	for i := 0; i+1 < len(frame); i += 2 {
		value := int16(t.amplitude * math.Sin(t.phase))
		frame[i] = value
		frame[i+1] = value
		t.phase += step
		if t.phase > 2*math.Pi {
			t.phase -= 2 * math.Pi
		}
	}
}

func sineFloatFrame(frequency, amplitude float64, phase *float64) []float64 {
	buf := make([]float64, frameSize*channels)
	step := 2 * math.Pi * frequency / dsp.SampleRate
	for i := 0; i+1 < len(buf); i += 2 {
		value := amplitude * math.Sin(*phase)
		buf[i] = value
		buf[i+1] = value
		*phase += step
	}
	return buf
}

func bufferRMS(buf []float64) float64 {
	if len(buf) == 0 {
		return 0
	}
	var sum float64
	for _, v := range buf {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(buf)))
}

func bufferPeak(buf []float64) float64 {
	peak := 0.0
	for _, v := range buf {
		if math.Abs(v) > peak {
			peak = math.Abs(v)
		}
	}
	return peak
}

func bufferFinite(buf []float64) bool {
	for _, v := range buf {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func filterResponse(setup func(*dsp.Biquad), frequency float64) float64 {
	var filter dsp.Biquad
	setup(&filter)
	phase := 0.0
	var lastRMS float64
	for frame := 0; frame < 25; frame++ {
		buf := sineFloatFrame(frequency, 10000, &phase)
		filter.ProcessStereo(buf)
		lastRMS = bufferRMS(buf)
	}
	return lastRMS / (10000 / math.Sqrt2)
}

func RunAutoMixChecks() []CheckResult {
	c := &checkCollector{}

	checkBiquads(c)
	checkDelayAndReverb(c)
	checkConversions(c)
	checkVolumeStyles(c)
	checkRecipeMatrix(c)
	checkLongWindows(c)
	checkEdgeWindows(c)
	checkTails(c)
	checkLoopBuffer(c)
	checkStyleNames(c)
	checkSelector(c)
	checkCamelot(c)
	checkKeyDetection(c)
	checkBeatAnalysis(c)
	checkRealtimeBudget(c)
	checkTempoSearchBounds(c)
	checkKeyContrastGate(c)
	checkStyleResolution(c)
	checkOutroResolution(c)
	checkTransitionTiming(c)
	checkAnalysisHelpers(c)
	checkAnnouncementGate(c)
	checkRestartStreamURL(c)
	checkTransitionSlide(c)
	checkAnalysisReadCap(c)

	return c.results
}
