package dsp_test

import (
	"math"
	"testing"

	"noraegaori/internal/audio/dsp"
)

func TestCurveStepPointsCubic(t *testing.T) {
	curve := dsp.Curve{{Start: 0, End: 1, Points: []dsp.Point{{X: 0, Y: 1}, {X: 0.5, Y: 1}, {X: 0.5, Y: 0.25}, {X: 1, Y: 0.25}}}}

	wants := []struct{ x, y float64 }{{0.297, 0.883}, {0.5, 0.625}, {0.703, 0.367}}
	for i := range wants {
		want := &wants[i]
		if got := curve.At(want.x); math.Abs(got-want.y) > 0.005 {
			t.Errorf("At(%.3f) = %.4f, want %.3f", want.x, got, want.y)
		}
	}
}

func TestCurveTwoPointLinear(t *testing.T) {
	curve := dsp.Curve{{Start: 0.2, End: 0.6, Points: []dsp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}}}

	wants := []struct{ x, y float64 }{{0.3, 0.25}, {0.4, 0.5}, {0.5, 0.75}}
	for i := range wants {
		want := &wants[i]
		if got := curve.At(want.x); math.Abs(got-want.y) > 1e-6 {
			t.Errorf("At(%.1f) = %.6f, want %.2f", want.x, got, want.y)
		}
	}
}

func TestCurveRampBisection(t *testing.T) {
	curve := dsp.Curve{{Start: 0, End: 1, Points: []dsp.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}}}

	wants := []struct{ x, y float64 }{
		{0.25, 549755813887.0 / 2199023255552},
		{0.3, 659706976665.0 / 2199023255552},
	}
	for i := range wants {
		want := &wants[i]
		if got := curve.At(want.x); got != want.y {
			t.Errorf("At(%v) = %v, want exactly %v", want.x, got, want.y)
		}
	}
}

func TestCurveTwoPointEarlyEnd(t *testing.T) {
	curve := dsp.Curve{{Start: 0, End: 1, Points: []dsp.Point{{X: 0, Y: 0}, {X: 0.5, Y: 1}}}}

	wants := []struct{ x, y float64 }{{0.1, 0.2}, {0.25, 0.5}, {0.4, 0.8}, {0.7, 1}}
	for i := range wants {
		want := &wants[i]
		if got := curve.At(want.x); math.Abs(got-want.y) > 1e-9 {
			t.Errorf("At(%.2f) = %.12f, want %.1f", want.x, got, want.y)
		}
	}
}

func TestCurveNinePointSegment(t *testing.T) {
	points := make([]dsp.Point, 9)
	for i := range points {
		points[i] = dsp.Point{X: float64(i) / 8, Y: 1 - float64(i)/8}
	}
	curve := dsp.Curve{{Start: 0, End: 1, Points: points}}

	for _, x := range []float64{0.1, 0.3, 0.7} {
		if got := curve.At(x); math.Abs(got-(1-x)) > 1e-9 {
			t.Errorf("At(%.1f) = %.12f, want %.1f", x, got, 1-x)
		}
	}
}

func TestCurveBlendsNegativeZero(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	curve := dsp.Curve{{Start: 0, End: 1, Points: []dsp.Point{{X: 0, Y: negativeZero}, {X: 0.5, Y: negativeZero}, {X: 1, Y: negativeZero}}}}

	if got := curve.At(0.4); got != 0 || math.Signbit(got) {
		t.Errorf("At(0.4) = %v (sign bit %v), want positive zero", got, math.Signbit(got))
	}
}

func TestCurveHoldsEnds(t *testing.T) {
	curve := dsp.Curve{
		{Start: 0, End: 0.5, Points: []dsp.Point{{X: 0, Y: 1}, {X: 1, Y: 1}}},
		{Start: 0.5, End: 1, Points: []dsp.Point{{X: 0, Y: 1}, {X: 1, Y: 0}}},
	}

	wants := []struct{ x, y float64 }{{-1, 1}, {0.25, 1}, {0.5, 1}, {0.75, 0.5}, {1, 0}, {2, 0}}
	for i := range wants {
		want := &wants[i]
		if got := curve.At(want.x); math.Abs(got-want.y) > 1e-6 {
			t.Errorf("At(%.2f) = %.6f, want %.2f", want.x, got, want.y)
		}
	}
}

func TestCurveSwitchesAtSegment(t *testing.T) {
	curve := dsp.Curve{
		{Start: 0, End: 0.5, Points: []dsp.Point{{X: 0, Y: 1}, {X: 1, Y: 1}}},
		{Start: 0.5, End: 1, Points: []dsp.Point{{X: 0, Y: 0}, {X: 1, Y: 0}}},
	}

	if got := curve.At(0.5); got != 0 {
		t.Errorf("At(0.5) = %v, want 0 from the segment starting there", got)
	}
}

func TestEmptyCurveIsZero(t *testing.T) {
	if got := (dsp.Curve{}).At(0.5); got != 0 {
		t.Errorf("At(0.5) = %v, want 0", got)
	}
}
