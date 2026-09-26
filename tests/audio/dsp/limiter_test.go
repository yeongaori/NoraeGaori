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

	first := constantFrame(10000)
	limiter.ProcessStereo(first, dsp.FullScale)
	if start := first[0]; start > 5100 {
		t.Errorf("right after the loud part the level is %.1f, want at most 5100 (gain still near 0.5)", start)
	}
	if end := first[len(first)-1]; end <= first[0]+1000 {
		t.Errorf("the first quiet frame rose from %.1f to %.1f, want a steady release", first[0], end)
	}

	var frame []float64
	for i := 0; i < 19; i++ {
		frame = constantFrame(10000)
		limiter.ProcessStereo(frame, dsp.FullScale)
	}

	if level := frame[len(frame)-1]; level < 9900 {
		t.Errorf("0.4s after the loud part the level is %.1f, want above 9900", level)
	}
}

func TestLimiterDoesNotStepTheGainAtAFrameBoundary(t *testing.T) {
	var limiter dsp.Limiter
	limiter.ProcessStereo(constantFrame(20000), dsp.FullScale)
	frame := constantFrame(20000)
	for i := 24 * dsp.Channels; i < len(frame); i++ {
		frame[i] = 2 * dsp.FullScale
	}

	limiter.ProcessStereo(frame, dsp.FullScale)

	previous := 20000.0
	for pair := 0; pair < 24; pair++ {
		sample := frame[pair*dsp.Channels]
		if drop := previous - sample; drop > 20000.0/48+1 {
			t.Fatalf("sample %d dropped by %.1f on a steady 20000 signal, want at most %.1f (a 1/48 gain ramp, not a step)", pair, drop, 20000.0/48+1)
		}
		previous = sample
	}
	if peak := audiotest.BufferPeak(frame); peak > dsp.FullScale {
		t.Errorf("peak %.1f, want at most %.1f", peak, dsp.FullScale)
	}
}

func TestLimiterSoftClipsWhatTheRampCannotCatch(t *testing.T) {
	var limiter dsp.Limiter
	limiter.ProcessStereo(constantFrame(20000), dsp.FullScale)
	frame := constantFrame(20000)
	for i := 5 * dsp.Channels; i < len(frame); i++ {
		frame[i] = 2 * dsp.FullScale
	}

	limiter.ProcessStereo(frame, dsp.FullScale)

	knee := 0.9 * dsp.FullScale
	for pair := 5; pair < 23; pair++ {
		if sample := frame[pair*dsp.Channels]; sample <= knee || sample >= dsp.FullScale {
			t.Fatalf("sample %d = %.1f, want soft-clipped between %.1f and %.1f while the gain ramp catches up", pair, sample, knee, dsp.FullScale)
		}
	}
	for pair := 30; pair < dsp.FrameSize; pair++ {
		if sample := frame[pair*dsp.Channels]; math.Abs(sample-dsp.FullScale) > 1 {
			t.Fatalf("sample %d = %.1f, want %.1f once the gain has settled at 0.5", pair, sample, dsp.FullScale)
		}
	}
}

func TestLimiterBecomesIdleAfterRelease(t *testing.T) {
	var limiter dsp.Limiter
	if !limiter.IsIdle() {
		t.Fatal("a fresh limiter is not idle")
	}

	limiter.ProcessStereo(constantFrame(2*dsp.FullScale), dsp.FullScale)
	if limiter.IsIdle() {
		t.Fatal("idle right after a 2x full-scale frame, want it still reducing")
	}

	var frame []float64
	for i := 0; i < 40; i++ {
		frame = constantFrame(10000)
		limiter.ProcessStereo(frame, dsp.FullScale)
	}
	if !limiter.IsIdle() {
		t.Fatal("still reducing 0.8s after the loud part, want idle")
	}
	for i, sample := range frame {
		if sample != 10000 {
			t.Fatalf("sample %d = %v once idle, want the untouched 10000", i, sample)
		}
	}
}

func TestLimiterWatchesBothChannels(t *testing.T) {
	var limiter dsp.Limiter
	frame := make([]float64, dsp.FrameSize*dsp.Channels)
	for i := 0; i < len(frame); i += dsp.Channels {
		frame[i] = 1000
		frame[i+1] = 2 * dsp.FullScale
	}

	limiter.ProcessStereo(frame, dsp.FullScale)

	for i := 1; i < len(frame); i += dsp.Channels {
		if frame[i] > dsp.FullScale+1e-9 {
			t.Fatalf("right sample %d = %.1f, want at most %.1f even when the left channel is quiet", i, frame[i], dsp.FullScale)
		}
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
