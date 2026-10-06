package dsp_test

import (
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestSilentEdgesCountTheQuietSamplesAtBothEnds(t *testing.T) {
	samples := []float32{0, 0.005, -0.009, 0.5, 0.02, -0.3, 0.004, 0}
	lead, trail := dsp.SilentEdges(samples)
	if lead != 3 || trail != 2 {
		t.Errorf("edges = (%d, %d), want (3, 2)", lead, trail)
	}
}

func TestSilentEdgesTreatTheThresholdAsSilent(t *testing.T) {
	lead, trail := dsp.SilentEdges([]float32{0.01, -0.01, 0.0101, -0.0101, 0.01})
	if lead != 2 || trail != 1 {
		t.Errorf("edges = (%d, %d), want (2, 1) with exactly 0.01 counted as silent", lead, trail)
	}
}

func TestSilentEdgesLeaveNoTrailWhenEverythingIsSilent(t *testing.T) {
	samples := []float32{0, 0.001, -0.002, 0}
	lead, trail := dsp.SilentEdges(samples)
	if lead != len(samples) || trail != 0 {
		t.Errorf("edges = (%d, %d), want (%d, 0) so the whole buffer is not trimmed twice", lead, trail, len(samples))
	}
	if lead, trail := dsp.SilentEdges(nil); lead != 0 || trail != 0 {
		t.Errorf("empty edges = (%d, %d), want (0, 0)", lead, trail)
	}
}
