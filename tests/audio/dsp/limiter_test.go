package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func constantFrame(value float64) []float64 {
	frame := make([]float64, dsp.FrameSize*dsp.Channels)
	for i := range frame {
		frame[i] = value
	}
	return frame
}

func TestLimiterLeavesQuietAudioUntouched(t *testing.T) {
	var limiter dsp.Limiter
	phase := 0.0
	frame := audiotest.SineFloatFrame(440, 20000, &phase)
	want := append([]float64(nil), frame...)

	limiter.ProcessStereo(frame, dsp.FullScale)

	for i := range frame {
		if frame[i] != want[i] {
			t.Fatalf("sample %d = %v, want untouched %v", i, frame[i], want[i])
		}
	}
}

func TestLimiterKeepsDoubleFullScaleUnderTheCeiling(t *testing.T) {
	var limiter dsp.Limiter
	phase := 0.0

	for frameIndex := 0; frameIndex < 20; frameIndex++ {
		frame := audiotest.SineFloatFrame(440, 2*dsp.FullScale, &phase)
		limiter.ProcessStereo(frame, dsp.FullScale)
		if peak := audiotest.BufferPeak(frame); peak > dsp.FullScale+1e-9 {
			t.Fatalf("frame %d peaked at %.1f, want at most %.1f", frameIndex, peak, dsp.FullScale)
		}
		run := 0
		for i := 0; i < len(frame); i += dsp.Channels {
			if math.Abs(frame[i]) >= dsp.FullScale-1 {
				run++
			} else {
				run = 0
			}
			if run > 2 {
				t.Fatalf("frame %d rode the ceiling for %d samples, want a held gain instead of a flattened waveform", frameIndex, run)
			}
		}
	}
}

func TestLimiterRampsDownBeforeASpike(t *testing.T) {
	var limiter dsp.Limiter
	frame := constantFrame(10000)
	spike := 500
	frame[spike*dsp.Channels] = 4 * dsp.FullScale

	limiter.ProcessStereo(frame, dsp.FullScale)

	if got := frame[spike*dsp.Channels]; got > dsp.FullScale+1e-9 {
		t.Fatalf("spike came out at %.1f, want at most %.1f", got, dsp.FullScale)
	}
	if before := frame[(spike-24)*dsp.Channels]; before >= 10000 {
		t.Errorf("half an attack before the spike the level is %.1f, want already reduced below 10000", before)
	}
	if early := frame[(spike-100)*dsp.Channels]; early != 10000 {
		t.Errorf("before the hold and attack windows the level is %.1f, want untouched 10000", early)
	}
	for i := spike - 100; i < spike; i++ {
		step := (frame[i*dsp.Channels] - frame[(i+1)*dsp.Channels]) / 10000
		if i+1 != spike && step > 1.0/48+1e-9 {
			t.Fatalf("gain dropped by %.4f between samples %d and %d, want a ramp", step, i, i+1)
		}
	}
}

func TestLimiterReleasesAfterTheLoudPart(t *testing.T) {
	var limiter dsp.Limiter
	limiter.ProcessStereo(constantFrame(2*dsp.FullScale), dsp.FullScale)

	var frame []float64
	for i := 0; i < 20; i++ {
		frame = constantFrame(10000)
		limiter.ProcessStereo(frame, dsp.FullScale)
	}

	if level := frame[len(frame)-1]; level < 9900 {
		t.Errorf("0.4s after the loud part the level is %.1f, want above 9900", level)
	}
}

func TestLimiterHandlesSilence(t *testing.T) {
	var limiter dsp.Limiter
	frame := constantFrame(0)

	limiter.ProcessStereo(frame, dsp.FullScale)

	for i, sample := range frame {
		if math.IsNaN(sample) || sample != 0 {
			t.Fatalf("sample %d = %v, want 0", i, sample)
		}
	}
}
