package audiotest

import (
	"math"
	"math/rand"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
)

type ToneGenerator struct {
	Frequency float64
	Amplitude float64
	phase     float64
}

func (t *ToneGenerator) Fill(frame []int16) {
	step := 2 * math.Pi * t.Frequency / dsp.SampleRate
	for i := 0; i+1 < len(frame); i += 2 {
		value := int16(t.Amplitude * math.Sin(t.phase))
		frame[i] = value
		frame[i+1] = value
		t.phase += step
		if t.phase > 2*math.Pi {
			t.phase -= 2 * math.Pi
		}
	}
}

func SineFloatFrame(frequency, amplitude float64, phase *float64) []float64 {
	buf := make([]float64, dsp.FrameSize*dsp.Channels)
	step := 2 * math.Pi * frequency / dsp.SampleRate
	for i := 0; i+1 < len(buf); i += 2 {
		value := amplitude * math.Sin(*phase)
		buf[i] = value
		buf[i+1] = value
		*phase += step
	}
	return buf
}

func BufferRMS(buf []float64) float64 {
	if len(buf) == 0 {
		return 0
	}
	var sum float64
	for _, value := range buf {
		sum += value * value
	}
	return math.Sqrt(sum / float64(len(buf)))
}

func BufferPeak(buf []float64) float64 {
	peak := 0.0
	for _, value := range buf {
		if math.Abs(value) > peak {
			peak = math.Abs(value)
		}
	}
	return peak
}

func IsBufferFinite(buf []float64) bool {
	for _, value := range buf {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func FilterResponse(setup func(*dsp.Biquad), frequency float64) float64 {
	var filter dsp.Biquad
	setup(&filter)
	phase := 0.0
	var lastRMS float64
	for frame := 0; frame < 25; frame++ {
		buf := SineFloatFrame(frequency, 10000, &phase)
		filter.ProcessStereo(buf)
		lastRMS = BufferRMS(buf)
	}
	return lastRMS / (10000 / math.Sqrt2)
}

func SynthesizeClickTrack(bpm, seconds, sampleRate float64, accentEvery, accentPhase int) []float32 {
	total := int(seconds * sampleRate)
	samples := make([]float32, total)
	interval := 60 / bpm * sampleRate
	burst := int(0.02 * sampleRate)
	source := rand.New(rand.NewSource(11))

	beat := 0
	for position := 0.0; position < float64(total); position += interval {
		amplitude := 0.5
		if accentEvery > 0 && beat%accentEvery == accentPhase {
			amplitude = 1.0
		}
		start := int(position)
		for i := 0; i < burst && start+i < total; i++ {
			decay := 1 - float64(i)/float64(burst)
			samples[start+i] += float32(amplitude * decay * source.NormFloat64() * 0.5)
		}
		beat++
	}
	return samples
}

func AnalyzeAccentedClickTrack(t *testing.T) *analysis.TrackAnalysis {
	t.Helper()

	track, err := analysis.AnalyzeTrackSamples(SynthesizeClickTrack(120, 40, analysis.SampleRate, 4, 0), analysis.SampleRate)
	if err != nil {
		t.Fatalf("accented click track failed: %v", err)
	}
	return track
}
