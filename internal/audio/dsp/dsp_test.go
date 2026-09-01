package dsp_test

import (
	"noraegaori/internal/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
)

func TestBiquadLowpassShape(t *testing.T) {
	passband := audiotest.FilterResponse(func(f *dsp.Biquad) { f.SetLowpass(500, 0.707) }, 100)
	stopband := audiotest.FilterResponse(func(f *dsp.Biquad) { f.SetLowpass(500, 0.707) }, 8000)

	if passband <= 0.85 {
		t.Errorf("100Hz gain = %.3f, want > 0.85", passband)
	}
	if stopband >= 0.05 {
		t.Errorf("8000Hz gain = %.4f, want < 0.05", stopband)
	}
}

func TestBiquadHighpassShape(t *testing.T) {
	passband := audiotest.FilterResponse(func(f *dsp.Biquad) { f.SetHighpass(2000, 0.707) }, 12000)
	stopband := audiotest.FilterResponse(func(f *dsp.Biquad) { f.SetHighpass(2000, 0.707) }, 100)

	if passband <= 0.85 {
		t.Errorf("12000Hz gain = %.3f, want > 0.85", passband)
	}
	if stopband >= 0.05 {
		t.Errorf("100Hz gain = %.4f, want < 0.05", stopband)
	}
}

func TestBiquadLowShelfKillsBass(t *testing.T) {
	setup := func(f *dsp.Biquad) { f.SetLowShelf(transition.EQLowFreq, transition.EQShelfQ, transition.EQKillDB) }
	bass := audiotest.FilterResponse(setup, 60)
	rest := audiotest.FilterResponse(setup, 6000)

	if bass >= 0.05 {
		t.Errorf("60Hz gain = %.4f, want < 0.05", bass)
	}
	if rest <= 0.9 {
		t.Errorf("6000Hz gain = %.3f, want > 0.9", rest)
	}
}

func TestBiquadHighShelfKillsTreble(t *testing.T) {
	setup := func(f *dsp.Biquad) { f.SetHighShelf(transition.EQHighFreq, transition.EQShelfQ, transition.EQKillDB) }
	treble := audiotest.FilterResponse(setup, 12000)
	rest := audiotest.FilterResponse(setup, 200)

	if treble >= 0.05 {
		t.Errorf("12000Hz gain = %.4f, want < 0.05", treble)
	}
	if rest <= 0.9 {
		t.Errorf("200Hz gain = %.3f, want > 0.9", rest)
	}
}

func TestBiquadPeakingCutsMids(t *testing.T) {
	setup := func(f *dsp.Biquad) { f.SetPeaking(transition.EQMidFreq, transition.EQMidQ, transition.EQKillDB) }

	if gain := audiotest.FilterResponse(setup, transition.EQMidFreq); gain >= 0.1 {
		t.Errorf("%.0fHz gain = %.4f, want < 0.1", transition.EQMidFreq, gain)
	}
}

func TestBiquadBypassIsTransparent(t *testing.T) {
	var bypass dsp.Biquad
	bypass.SetBypass()

	phase := 0.0
	original := audiotest.SineFloatFrame(1000, 9000, &phase)
	processed := make([]float64, len(original))
	copy(processed, original)
	bypass.ProcessStereo(processed)

	for i := range original {
		if original[i] != processed[i] {
			t.Fatalf("sample %d changed from %g to %g", i, original[i], processed[i])
		}
	}
}

func TestBiquadExtremeParametersStayStable(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*dsp.Biquad)
	}{
		{"lowpass 0.1Hz", func(f *dsp.Biquad) { f.SetLowpass(0.1, 0.707) }},
		{"lowpass 96000Hz", func(f *dsp.Biquad) { f.SetLowpass(96000, 0.707) }},
		{"highpass 0Hz", func(f *dsp.Biquad) { f.SetHighpass(0, 0) }},
		{"peaking negative Q", func(f *dsp.Biquad) { f.SetPeaking(1000, -5, -40) }},
		{"lowshelf huge gain", func(f *dsp.Biquad) { f.SetLowShelf(250, 0.707, 120) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var filter dsp.Biquad
			testCase.setup(&filter)

			phase := 0.0
			for frame := 0; frame < 50; frame++ {
				buf := audiotest.SineFloatFrame(440, 10000, &phase)
				filter.ProcessStereo(buf)

				if !audiotest.IsBufferFinite(buf) {
					t.Fatalf("frame %d produced non-finite output", frame)
				}
				if peak := audiotest.BufferPeak(buf); peak > 1e9 {
					t.Fatalf("frame %d diverged to peak %g", frame, peak)
				}
			}
		})
	}
}

func newEchoDelayLine() *dsp.DelayLine {
	delay := dsp.NewDelayLine()
	delay.SetDelaySeconds(0.1)
	delay.Feedback = 0.5
	delay.Wet = 1
	delay.Dry = 1

	impulse := make([]float64, dsp.FrameSize*dsp.Channels)
	impulse[0] = 10000
	impulse[1] = 10000
	delay.ProcessStereo(impulse)

	return delay
}

func TestDelayLineEchoTiming(t *testing.T) {
	delay := newEchoDelayLine()
	silent := make([]float64, dsp.FrameSize*dsp.Channels)

	firstEchoFrame := 0
	firstEchoPeak := 0.0
	for frame := 1; frame <= 10; frame++ {
		dsp.SilenceFloat(silent)
		delay.ProcessStereo(silent)
		if peak := audiotest.BufferPeak(silent); peak > 100 && firstEchoFrame == 0 {
			firstEchoFrame = frame
			firstEchoPeak = peak
		}
	}

	if firstEchoFrame != 5 {
		t.Errorf("first echo at frame %d (peak %.0f), want frame 5 for 100ms", firstEchoFrame, firstEchoPeak)
	}
}

func TestDelayLineFeedbackDecays(t *testing.T) {
	delay := newEchoDelayLine()
	silent := make([]float64, dsp.FrameSize*dsp.Channels)

	for frame := 1; frame <= 10; frame++ {
		dsp.SilenceFloat(silent)
		delay.ProcessStereo(silent)
	}

	var peaks []float64
	for frame := 0; frame < 20; frame++ {
		dsp.SilenceFloat(silent)
		delay.ProcessStereo(silent)
		if peak := audiotest.BufferPeak(silent); peak > 1 {
			peaks = append(peaks, peak)
		}
	}

	if len(peaks) == 0 {
		t.Fatal("no echo peaks observed")
	}
	for i := 1; i < len(peaks); i++ {
		if peaks[i] > peaks[i-1]*1.05 {
			t.Errorf("echo %d grew from %.1f to %.1f", i, peaks[i-1], peaks[i])
		}
	}
}

func newImpulsedReverb() (*dsp.Reverb, []float64) {
	unit := dsp.NewReverb()

	impulse := make([]float64, dsp.FrameSize*dsp.Channels)
	impulse[0] = 20000
	impulse[1] = 20000
	unit.ProcessStereo(impulse, 0, 1)

	return unit, make([]float64, dsp.FrameSize*dsp.Channels)
}

func TestReverbProducesBoundedTail(t *testing.T) {
	unit, silent := newImpulsedReverb()

	tailEnergy := 0.0
	maxTailPeak := 0.0
	for frame := 0; frame < 60; frame++ {
		dsp.SilenceFloat(silent)
		unit.ProcessStereo(silent, 0, 1)

		if !audiotest.IsBufferFinite(silent) {
			t.Fatalf("frame %d produced non-finite output", frame)
		}
		tailEnergy += audiotest.BufferRMS(silent)
		if peak := audiotest.BufferPeak(silent); peak > maxTailPeak {
			maxTailPeak = peak
		}
	}

	if tailEnergy <= 0 {
		t.Errorf("tail energy = %.1f, want > 0", tailEnergy)
	}
	if maxTailPeak >= 40000 {
		t.Errorf("tail peak = %.0f, want < 40000", maxTailPeak)
	}
}

func TestReverbTailDecaysToNearSilence(t *testing.T) {
	unit, silent := newImpulsedReverb()

	late := 0.0
	for frame := 0; frame < 460; frame++ {
		dsp.SilenceFloat(silent)
		unit.ProcessStereo(silent, 0, 1)
		late = audiotest.BufferRMS(silent)
	}

	if late >= 1 {
		t.Errorf("rms after 460 frames = %.6f, want < 1", late)
	}
}

func TestFloatToFrameClamps(t *testing.T) {
	src := []float64{40000, -40000, 100.4, -100.4, 0}
	dst := make([]int16, len(src))
	dsp.FloatToFrame(src, dst)

	for _, want := range []struct {
		index int
		value int16
	}{{0, 32767}, {1, -32768}, {2, 100}, {4, 0}} {
		if dst[want.index] != want.value {
			t.Errorf("dst[%d] = %d, want %d (full: %v)", want.index, dst[want.index], want.value, dst)
		}
	}
}

func TestFrameToFloatZeroPads(t *testing.T) {
	out := make([]float64, 5)
	dsp.FrameToFloat([]int16{1000, -1000, 32767}, out)

	for _, want := range []struct {
		index int
		value float64
	}{{0, 1000}, {2, 32767}, {3, 0}, {4, 0}} {
		if out[want.index] != want.value {
			t.Errorf("out[%d] = %g, want %g (full: %v)", want.index, out[want.index], want.value, out)
		}
	}
}

func TestGainRampEndpoints(t *testing.T) {
	ramp := make([]float64, dsp.FrameSize*dsp.Channels)
	for i := range ramp {
		ramp[i] = 1000
	}
	dsp.ApplyGainRamp(ramp, 0, 1)

	if ramp[0] != 0 {
		t.Errorf("first sample = %.1f, want 0", ramp[0])
	}
	if last := ramp[len(ramp)-1]; last <= 900 || last > 1000 {
		t.Errorf("last sample = %.1f, want within (900, 1000]", last)
	}
}

func TestGainRampConstantFactor(t *testing.T) {
	flat := make([]float64, 8)
	for i := range flat {
		flat[i] = 500
	}
	dsp.ApplyGainRamp(flat, 0.5, 0.5)

	for i, value := range flat {
		if value != 250 {
			t.Errorf("flat[%d] = %g, want 250 (full: %v)", i, value, flat)
		}
	}
}
