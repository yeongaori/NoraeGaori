package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestReverbStaysSilentThroughThePredelay(t *testing.T) {
	output := renderImpulse(dsp.PrepareReverb(), 5)
	predelay := int(0.045 * dsp.SampleRate)

	if early := audiotest.BufferPeak(output[:predelay*dsp.Channels]); early != 0 {
		t.Errorf("output inside the 45ms predelay peaks at %.3f, want silence", early)
	}
	if late := audiotest.BufferPeak(output[predelay*dsp.Channels:]); late == 0 {
		t.Error("no output after the predelay")
	}
}

func TestReverbBuildsUpAndPeaksAfterThePredelay(t *testing.T) {
	output := renderImpulse(dsp.PrepareReverb(), 25)
	window := int(0.005 * dsp.SampleRate)

	peakAt, peakEnergy := 0, 0.0
	for start := 0; start+window < len(output)/dsp.Channels; start += window {
		energy := audiotest.BufferRMS(output[start*dsp.Channels : (start+window)*dsp.Channels])
		if energy > peakEnergy {
			peakAt, peakEnergy = start, energy
		}
	}
	if peakMs := float64(peakAt) * 1000 / dsp.SampleRate; peakMs < 50 || peakMs > 150 {
		t.Errorf("energy peaks at %.0fms, want a diffuse build-up peaking between 50 and 150ms", peakMs)
	}
}

func TestReverbDecaysAtTheMeasuredRate(t *testing.T) {
	output := renderImpulse(dsp.PrepareReverb(), 200)
	level := func(second float64) float64 {
		start := int(second*dsp.SampleRate) * dsp.Channels
		return audiotest.BufferRMS(output[start : start+int(0.2*dsp.SampleRate)*dsp.Channels])
	}

	perSecond := audiotest.Decibels(level(1)/level(3)) / 2
	if math.Abs(perSecond-7) > 1.5 {
		t.Errorf("tail decays %.1f dB per second, want about 7 dB (RT60 near 8.5s)", perSecond)
	}
}

func TestReverbChannelsAreDecorrelated(t *testing.T) {
	output := renderImpulse(dsp.PrepareReverb(), 100)

	var leftRight, leftLeft, rightRight float64
	for i := 0; i+1 < len(output); i += dsp.Channels {
		leftRight += output[i] * output[i+1]
		leftLeft += output[i] * output[i]
		rightRight += output[i+1] * output[i+1]
	}
	if correlation := leftRight / math.Sqrt(leftLeft*rightRight); math.Abs(correlation) > 0.05 {
		t.Errorf("left/right correlation %.3f, want near 0 like the measured 0.02", correlation)
	}
}

func TestReverbWetSitsThirteenDecibelsUnderPinkNoise(t *testing.T) {
	reverb := dsp.PrepareReverb()
	noise := &audiotest.PinkNoise{}
	input := make([]float64, dsp.FrameSize*dsp.Channels)
	wet := make([]float64, len(input))

	var inputEnergy, wetEnergy float64
	for frame := 0; frame < 1000; frame++ {
		noise.Fill(input, 8000)
		reverb.Render(input, wet)
		if frame >= 500 {
			inputEnergy += audiotest.BufferRMS(input)
			wetEnergy += audiotest.BufferRMS(wet)
		}
	}
	if level := audiotest.Decibels(wetEnergy / inputEnergy); math.Abs(level+13) > 1.5 {
		t.Errorf("wet level %+.1f dB against the dry input, want about -13 dB", level)
	}
}

func TestReverbCutsTheLows(t *testing.T) {
	low := audiotest.ToneResponse(reverbWet(), 50)
	mid := audiotest.ToneResponse(reverbWet(), 1000)

	if audiotest.Decibels(low/mid) > -9 {
		t.Errorf("50Hz reverb is %+.1f dB against 1kHz, want the send high-pass to cut it", audiotest.Decibels(low/mid))
	}
}

func reverbWet() func([]float64) {
	reverb := dsp.PrepareReverb()
	wet := make([]float64, dsp.FrameSize*dsp.Channels)
	return func(buf []float64) {
		reverb.Render(buf, wet)
		copy(buf, wet)
	}
}
