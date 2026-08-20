package player

import (
	"math"
	"testing"

	"noraegaori/internal/audio/dsp"
)

type checkCollector struct{ t *testing.T }

func (c *checkCollector) add(name string, passed bool, format string, args ...interface{}) {
	c.t.Helper()
	c.t.Run(name, func(t *testing.T) {
		t.Helper()
		if !passed {
			t.Errorf(format, args...)
		}
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

func TestCheckStyleResolution(t *testing.T)  { checkStyleResolution(&checkCollector{t: t}) }
func TestCheckOutroResolution(t *testing.T)  { checkOutroResolution(&checkCollector{t: t}) }
func TestCheckTransitionTiming(t *testing.T) { checkTransitionTiming(&checkCollector{t: t}) }
func TestCheckAnalysisHelpers(t *testing.T)  { checkAnalysisHelpers(&checkCollector{t: t}) }
func TestCheckAnnouncementGate(t *testing.T) { checkAnnouncementGate(&checkCollector{t: t}) }
func TestCheckRestartStreamURL(t *testing.T) { checkRestartStreamURL(&checkCollector{t: t}) }
func TestCheckTransitionSlide(t *testing.T)  { checkTransitionSlide(&checkCollector{t: t}) }
func TestCheckAnalysisReadCap(t *testing.T)  { checkAnalysisReadCap(&checkCollector{t: t}) }
