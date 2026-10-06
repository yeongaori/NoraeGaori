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
