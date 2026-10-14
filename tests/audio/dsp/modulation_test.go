package dsp_test

import (
	"math"
	"noraegaori/tests/testutil/audiotest"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestPhaserAtZeroMixIsBitExact(t *testing.T) {
	phaser := dsp.PreparePhaser(0.5)
	phase := 0.0
	for frame := 0; frame < 20; frame++ {
		block := audiotest.SineFloatFrame(700, 9000, &phase)
		dry := append([]float64(nil), block...)
		phaser.Process(block, 0, 0)
		for i := range block {
			if block[i] != dry[i] {
				t.Fatalf("frame %d sample %d = %v, want the untouched %v", frame, i, block[i], dry[i])
			}
		}
	}
}

func TestPhaserSweepsBetweenItsCorners(t *testing.T) {
	if low := dsp.PhaserFrequency(-math.Pi / 2); math.Abs(low-300) > 1e-9 {
		t.Errorf("lowest sweep frequency = %.3f, want 300 Hz", low)
	}
	if high := dsp.PhaserFrequency(math.Pi / 2); math.Abs(high-3000) > 1e-6 {
		t.Errorf("highest sweep frequency = %.3f, want 3000 Hz", high)
	}
	for _, want := range []struct{ phase, frequency float64 }{{-math.Pi / 6, 533.4838230116768}, {math.Pi / 6, 1687.0239755710473}} {
		if got := dsp.PhaserFrequency(want.phase); math.Abs(got-want.frequency) > 1e-6 {
			t.Errorf("sweep frequency at phase %.4f = %.6f, want %.6f on the logarithmic sweep", want.phase, got, want.frequency)
		}
	}
}

func TestPhaserSweepsTheChannelsInQuadrature(t *testing.T) {
	phaser := dsp.PreparePhaser(1)
	phase := 0.0
	var difference, level float64
	for frame := 0; frame < 25; frame++ {
		block := audiotest.SineFloatFrame(1000, 9000, &phase)
		phaser.Process(block, 1, 1)
		for i := 0; i < len(block); i += dsp.Channels {
			difference += (block[i] - block[i+1]) * (block[i] - block[i+1])
			level += block[i] * block[i]
		}
	}
	if ratio := math.Sqrt(difference / level); ratio < 0.5 {
		t.Errorf("left and right differ by %.3f of the level, want the right sweep a quarter cycle apart", ratio)
	}
}

func TestPhaserKeepsASilentChannelSilent(t *testing.T) {
	phaser := dsp.PreparePhaser(0.5)
	block := make([]float64, dsp.FrameSize*dsp.Channels)
	for frame := 0; frame < 5; frame++ {
		for i := 0; i < len(block); i += dsp.Channels {
			block[i] = 9000 * math.Sin(2*math.Pi*700*float64(frame*dsp.FrameSize+i/dsp.Channels)/dsp.SampleRate)
			block[i+1] = 0
		}
		phaser.Process(block, 0.7, 0.7)
		for i := 1; i < len(block); i += dsp.Channels {
			if block[i] != 0 {
				t.Fatalf("frame %d right sample %d = %v, want the silent channel untouched by the left", frame, i, block[i])
			}
		}
	}
}

func TestPhaserNotchMovesAcrossATone(t *testing.T) {
	phaser := dsp.PreparePhaser(1)
	phase := 0.0
	quietest, loudest := math.Inf(1), 0.0
	for frame := 0; frame < 100; frame++ {
		block := audiotest.SineFloatFrame(1000, 9000, &phase)
		phaser.Process(block, 0.5, 0.5)
		if !audiotest.IsBufferFinite(block) {
			t.Fatalf("frame %d is not finite", frame)
		}
		if frame < 10 {
			continue
		}
		level := audiotest.BufferRMS(block)
		quietest = math.Min(quietest, level)
		loudest = math.Max(loudest, level)
	}
	if swing := audiotest.Decibels(loudest / quietest); swing < 6 {
		t.Errorf("1kHz level swings by %.1f dB over one LFO cycle, want a notch passing through it", swing)
	}
	if loudest > 9000*1.5 {
		t.Errorf("1kHz peaks at %.0f RMS, want the phaser to stay near the input level", loudest)
	}
}

func TestBitcrusherAtZeroAmountIsBitExact(t *testing.T) {
	crusher := &dsp.Bitcrusher{}
	phase := 0.0
	block := audiotest.SineFloatFrame(700, 9000.37, &phase)
	dry := append([]float64(nil), block...)
	crusher.Process(block, 0, 0)
	for i := range block {
		if block[i] != dry[i] {
			t.Fatalf("sample %d = %v, want the untouched %v", i, block[i], dry[i])
		}
	}
}

func TestFullBitcrushHoldsAndQuantizes(t *testing.T) {
	if dsp.CrushQuantum(0) != 1 || dsp.CrushQuantum(1) != 1024 || dsp.CrushHold(0) != 1 || dsp.CrushHold(1) != 8 {
		t.Fatalf("quantum %v..%v and hold %d..%d, want 1..1024 and 1..8",
			dsp.CrushQuantum(0), dsp.CrushQuantum(1), dsp.CrushHold(0), dsp.CrushHold(1))
	}

	crusher := &dsp.Bitcrusher{}
	phase := 0.0
	block := audiotest.SineFloatFrame(700, 9000, &phase)
	crusher.Process(block, 1, 1)
	for i := 0; i < len(block); i += dsp.Channels {
		if math.Mod(block[i], 1024) != 0 {
			t.Fatalf("sample %d = %v, want a multiple of the 6-bit step 1024", i/dsp.Channels, block[i])
		}
		if group := (i / dsp.Channels) % 8; group != 0 && block[i] != block[i-dsp.Channels] {
			t.Fatalf("sample %d = %v changed inside its 8-sample hold", i/dsp.Channels, block[i])
		}
	}
}
