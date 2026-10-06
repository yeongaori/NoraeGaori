package dsp_test

import (
	"math"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestStepPointsRenderAsTheMeasuredCubicBezier(t *testing.T) {
	curve := dsp.Curve{{Start: 0, End: 1, Points: []dsp.Point{{X: 0, Y: 1}, {X: 0.5, Y: 1}, {X: 0.5, Y: 0.25}, {X: 1, Y: 0.25}}}}

	for _, want := range []struct{ x, y float64 }{{0.297, 0.883}, {0.5, 0.625}, {0.703, 0.367}} {
		if got := curve.At(want.x); math.Abs(got-want.y) > 0.005 {
			t.Errorf("At(%.3f) = %.4f, want %.3f", want.x, got, want.y)
		}
	}
}

func TestTwoPointSegmentIsLinear(t *testing.T) {
	curve := dsp.Curve{{Start: 0.2, End: 0.6, Points: []dsp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}}}

	for _, want := range []struct{ x, y float64 }{{0.3, 0.25}, {0.4, 0.5}, {0.5, 0.75}} {
		if got := curve.At(want.x); math.Abs(got-want.y) > 1e-6 {
			t.Errorf("At(%.1f) = %.6f, want %.2f", want.x, got, want.y)
		}
	}
}

func TestCurveHoldsItsEndsOutsideTheSegments(t *testing.T) {
	curve := dsp.Curve{
		{Start: 0, End: 0.5, Points: []dsp.Point{{X: 0, Y: 1}, {X: 1, Y: 1}}},
		{Start: 0.5, End: 1, Points: []dsp.Point{{X: 0, Y: 1}, {X: 1, Y: 0}}},
	}

	for _, want := range []struct{ x, y float64 }{{-1, 1}, {0.25, 1}, {0.5, 1}, {0.75, 0.5}, {1, 0}, {2, 0}} {
		if got := curve.At(want.x); math.Abs(got-want.y) > 1e-6 {
			t.Errorf("At(%.2f) = %.6f, want %.2f", want.x, got, want.y)
		}
	}
}

func TestEmptyCurveIsZero(t *testing.T) {
	if got := (dsp.Curve{}).At(0.5); got != 0 {
		t.Errorf("At(0.5) = %v, want 0", got)
	}
}
