package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestFilterLowpassShape(t *testing.T) {
	passband := audiotest.FilterResponse(func(f *dsp.Filter) { f.SetLowpass(500, 0.707) }, 100)
	stopband := audiotest.FilterResponse(func(f *dsp.Filter) { f.SetLowpass(500, 0.707) }, 8000)

	if passband <= 0.85 {
		t.Errorf("100Hz gain = %.3f, want > 0.85", passband)
	}
	if stopband >= 0.05 {
		t.Errorf("8000Hz gain = %.4f, want < 0.05", stopband)
	}
}

func TestFilterHighpassShape(t *testing.T) {
	passband := audiotest.FilterResponse(func(f *dsp.Filter) { f.SetHighpass(2000, 0.707) }, 12000)
	stopband := audiotest.FilterResponse(func(f *dsp.Filter) { f.SetHighpass(2000, 0.707) }, 100)

	if passband <= 0.85 {
		t.Errorf("12000Hz gain = %.3f, want > 0.85", passband)
	}
	if stopband >= 0.05 {
		t.Errorf("100Hz gain = %.4f, want < 0.05", stopband)
	}
}

func TestFilterBandpassPeaksAtItsCentre(t *testing.T) {
	setup := func(f *dsp.Filter) { f.SetBandpass(1000, 5) }
	centre := audiotest.FilterResponse(setup, 1000)
	away := audiotest.FilterResponse(setup, 4000)

	if centre < 0.95 || centre > 1.05 {
		t.Errorf("1000Hz gain = %.3f, want about 1", centre)
	}
	if away >= 0.1 {
		t.Errorf("4000Hz gain = %.3f, want < 0.1", away)
	}
}

func TestFilterAllpassKeepsTheLevel(t *testing.T) {
	for _, frequency := range []float64{100, 4000, 12000} {
		gain := audiotest.FilterResponse(func(f *dsp.Filter) { f.SetAllpass(4000, 0.707) }, frequency)
		if gain < 0.98 || gain > 1.02 {
			t.Errorf("%.0fHz gain = %.3f, want 1", frequency, gain)
		}
	}
}

func TestFilterBypassIsTransparent(t *testing.T) {
	var bypass dsp.Filter
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

func TestFilterExtremeParametersStayStable(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*dsp.Filter)
	}{
		{"lowpass 0.1Hz", func(f *dsp.Filter) { f.SetLowpass(0.1, 0.707) }},
		{"lowpass 96000Hz", func(f *dsp.Filter) { f.SetLowpass(96000, 0.707) }},
		{"highpass 0Hz", func(f *dsp.Filter) { f.SetHighpass(0, 0) }},
		{"bandpass negative Q", func(f *dsp.Filter) { f.SetBandpass(1000, -5) }},
		{"allpass 96000Hz", func(f *dsp.Filter) { f.SetAllpass(96000, 0.707) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var filter dsp.Filter
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

func TestDryWetFollowsTheSquareRootSineLaw(t *testing.T) {
	cases := []struct{ level, dry, wet float64 }{
		{0, 1, 0},
		{0.5, 0.8409, 0.8409},
		{1, 0, 1},
	}
	for _, want := range cases {
		dry, wet := dsp.DryWet(want.level)
		if math.Abs(dry-want.dry) > 1e-3 || math.Abs(wet-want.wet) > 1e-3 {
			t.Errorf("DryWet(%.1f) = %.4f, %.4f, want %.4f, %.4f", want.level, dry, wet, want.dry, want.wet)
		}
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

func TestFrameConversionsPadAShortSourceWithSilence(t *testing.T) {
	floats := make([]float64, 300)
	for i := range floats {
		floats[i] = 5
	}
	dsp.FrameToFloat([]int16{100, -200, 300}, floats)
	samples := make([]int16, 300)
	for i := range samples {
		samples[i] = 5
	}
	dsp.FloatToFrame([]float64{100, -200, 300}, samples)

	for i := range floats {
		want := 0.0
		if i < 3 {
			want = []float64{100, -200, 300}[i]
		}
		if floats[i] != want || float64(samples[i]) != want {
			t.Fatalf("sample %d = %v as float and %v as int16, want %v", i, floats[i], samples[i], want)
		}
	}
}

func TestMixWetRampsEachChannelFromItsOwnWet(t *testing.T) {
	block := make([]float64, 8)
	wet := []float64{1000, 0, 1000, 0, 1000, 0, 1000, 0}
	dsp.MixWet(block, wet, 1, 1, 0, 1)

	for step, want := range []float64{250, 500, 750, 1000} {
		if left := block[step*dsp.Channels]; left != want {
			t.Errorf("left step %d = %v, want %v", step, left, want)
		}
		if right := block[step*dsp.Channels+1]; right != 0 {
			t.Errorf("right step %d = %v, want 0 from its silent wet", step, right)
		}
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
