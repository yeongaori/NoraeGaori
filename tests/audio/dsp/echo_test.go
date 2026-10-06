package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func renderImpulse(effect interface{ Render(input, output []float64) }, frames int) []float64 {
	size := dsp.FrameSize * dsp.Channels
	output := make([]float64, 0, frames*size)
	input := make([]float64, size)
	wet := make([]float64, size)
	for frame := 0; frame < frames; frame++ {
		dsp.SilenceFloat(input)
		if frame == 0 {
			input[0], input[1] = 20000, 20000
		}
		effect.Render(input, wet)
		output = append(output, wet...)
	}
	return output
}

func repeatPeaks(output []float64, delaySamples, count int) []float64 {
	peaks := make([]float64, 0, count)
	for repeat := 1; repeat <= count; repeat++ {
		start := repeat * delaySamples * dsp.Channels
		peaks = append(peaks, audiotest.BufferPeak(output[start:start+400]))
	}
	return peaks
}

func TestEchoRepeatsOnTheBeatAndFadesByTheMeasuredFeedback(t *testing.T) {
	output := renderImpulse(dsp.PrepareEcho(0.1, dsp.EchoFeedback), 50)

	if early := audiotest.BufferPeak(output[:4800*dsp.Channels]); early != 0 {
		t.Errorf("output before the first repeat peaks at %.1f, want silence", early)
	}
	peaks := repeatPeaks(output, 4800, 5)
	for i := 1; i < len(peaks); i++ {
		ratio := peaks[i] / peaks[i-1]
		if math.Abs(ratio-dsp.EchoFeedback) > 0.05 {
			t.Errorf("repeat %d is %.3f of the one before, want about %.2f", i+1, ratio, dsp.EchoFeedback)
		}
	}
}

func TestDelayRepeatsOnce(t *testing.T) {
	output := renderImpulse(dsp.PrepareEcho(0.1, 0), 50)
	peaks := repeatPeaks(output, 4800, 3)

	if peaks[0] < 1000 {
		t.Errorf("first repeat peaks at %.1f, want an audible tap", peaks[0])
	}
	if peaks[1] > 1 || peaks[2] > 1 {
		t.Errorf("later repeats peak at %.1f and %.1f, want none", peaks[1], peaks[2])
	}
}

func TestEchoThinsTheLows(t *testing.T) {
	low := audiotest.ToneResponse(echoWet(), 50)
	mid := audiotest.ToneResponse(echoWet(), 2000)

	if audiotest.Decibels(low/mid) > -9 {
		t.Errorf("50Hz echo is %+.1f dB against 2kHz, want the loop high-pass to cut it", audiotest.Decibels(low/mid))
	}
}

func TestEchoKeepsTheInputStereo(t *testing.T) {
	echo := dsp.PrepareEcho(0.01, dsp.EchoFeedback)
	input := make([]float64, dsp.FrameSize*dsp.Channels)
	wet := make([]float64, len(input))
	for i := 0; i < len(input); i += dsp.Channels {
		input[i] = 8000 * math.Sin(float64(i)/40)
	}
	echo.Render(input, wet)
	echo.Render(input, wet)

	var right float64
	for i := 1; i < len(wet); i += dsp.Channels {
		right = math.Max(right, math.Abs(wet[i]))
	}
	if right != 0 {
		t.Errorf("right channel echo peaks at %.1f, want the left-only input to stay left", right)
	}
}

func TestShortenedDelayRepeatsAtTheNewTime(t *testing.T) {
	echo := dsp.PrepareEcho(0.1, 0)
	echo.SetDelay(0.05)
	output := renderImpulse(echo, 20)

	if late := audiotest.BufferPeak(output[4700*dsp.Channels : 4900*dsp.Channels]); late > 50 {
		t.Errorf("output at the original 4800-sample delay peaks at %.1f, want the tap moved to 2400 samples", late)
	}
	if tap := audiotest.BufferPeak(output[2350*dsp.Channels : 2450*dsp.Channels]); tap < 15000 {
		t.Errorf("tap around 2400 samples peaks at %.1f, want the impulse", tap)
	}
}

func TestDelayCannotGrowPastItsBuffer(t *testing.T) {
	echo := dsp.PrepareEcho(0.05, 0)
	echo.SetDelay(1)
	output := renderImpulse(echo, 10)

	if tap := audiotest.BufferPeak(output[2350*dsp.Channels : 2450*dsp.Channels]); tap < 15000 {
		t.Errorf("tap around 2400 samples peaks at %.1f, want the delay held at its 0.05s buffer", tap)
	}
}

func TestGlidingDelayStaysSmooth(t *testing.T) {
	echo := dsp.PrepareEcho(0.1, 0)
	wet := make([]float64, dsp.FrameSize*dsp.Channels)
	phase := 0.0
	maxStep, previous := 0.0, 0.0
	for frame := 0; frame < 60; frame++ {
		input := audiotest.SineFloatFrame(440, 8000, &phase)
		echo.SetDelay(0.1 * math.Pow(1.0/16, float64(frame)/60))
		echo.Render(input, wet)
		if !audiotest.IsBufferFinite(wet) {
			t.Fatalf("frame %d is not finite", frame)
		}
		for i := 0; i < len(wet); i += dsp.Channels {
			if frame > 6 {
				maxStep = math.Max(maxStep, math.Abs(wet[i]-previous))
			}
			previous = wet[i]
		}
	}
	if limit := 3 * 2 * math.Pi * 440 / dsp.SampleRate * 8000; maxStep > limit {
		t.Errorf("gliding delay steps by %.0f between samples, want at most %.0f", maxStep, limit)
	}
}

func echoWet() func([]float64) {
	echo := dsp.PrepareEcho(0.005, dsp.EchoFeedback)
	wet := make([]float64, dsp.FrameSize*dsp.Channels)
	return func(buf []float64) {
		echo.Render(buf, wet)
		copy(buf, wet)
	}
}
