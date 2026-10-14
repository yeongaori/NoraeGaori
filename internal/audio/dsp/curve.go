package dsp

import "math"

const (
	curveSolveSteps   = 40
	curveInlinePoints = 8
)

type Point struct {
	X float64
	Y float64
}

type Segment struct {
	Start  float64
	End    float64
	Points []Point
}

type Curve []Segment

func (c Curve) At(progress float64) float64 {
	if len(c) == 0 {
		return 0
	}
	index := 0
	for i := 1; i < len(c); i++ {
		if progress < c[i].Start {
			break
		}
		index = i
	}
	segment := &c[index]
	local := 1.0
	if width := segment.End - segment.Start; width > 0 {
		local = ClampUnit((progress - segment.Start) / width)
	}
	return segment.valueAt(local)
}

func (s *Segment) valueAt(x float64) float64 {
	points := s.Points
	switch len(points) {
	case 0:
		return 0
	case 1:
		return points[0].Y
	}
	if last := points[len(points)-1]; x >= last.X {
		return last.Y
	}
	if first := points[0]; x <= first.X {
		return first.Y
	}
	if len(points) > curveInlinePoints {
		u := bisectBlend(x, func(u float64) float64 {
			return blendAt(points, u).X
		})
		return blendAt(points, u).Y
	}

	var xs, ys [curveInlinePoints]float64
	for i, point := range points {
		xs[i], ys[i] = point.X, point.Y
	}
	count := len(points)
	if isLevel(ys, count) {
		return ys[0]
	}
	if count == 2 && xs[0] == 0 && xs[1] == 1 && x > 0 && x < 1 {
		return blendCoordinate(ys, count, bisectLinear(x))
	}
	u := bisectBlend(x, func(u float64) float64 {
		return blendCoordinate(xs, count, u)
	})
	return blendCoordinate(ys, count, u)
}

func isLevel(values [curveInlinePoints]float64, count int) bool {
	level := values[0]
	if math.IsInf(level, 0) || math.IsNaN(level) || (level == 0 && math.Signbit(level)) {
		return false
	}
	for _, value := range values[1:count] {
		if math.Float64bits(value) != math.Float64bits(level) {
			return false
		}
	}
	return true
}

func bisectLinear(x float64) float64 {
	scale := float64(uint64(1) << curveSolveSteps)
	return (2*math.Ceil(x*scale) - 1) / (2 * scale)
}

func bisectBlend(x float64, positionAt func(float64) float64) float64 {
	low, high := 0.0, 1.0
	for step := 0; step < curveSolveSteps; step++ {
		middle := (low + high) / 2
		if positionAt(middle) < x {
			low = middle
		} else {
			high = middle
		}
	}
	return (low + high) / 2
}

func blendCoordinate(coordinates [curveInlinePoints]float64, count int, u float64) float64 {
	for size := count - 1; size > 0; size-- {
		for i := 0; i < size; i++ {
			coordinates[i] += (coordinates[i+1] - coordinates[i]) * u
		}
	}
	return coordinates[0]
}

func blendAt(points []Point, u float64) Point {
	work := append(make([]Point, 0, len(points)), points...)
	for size := len(work) - 1; size > 0; size-- {
		for i := 0; i < size; i++ {
			work[i].X += (work[i+1].X - work[i].X) * u
			work[i].Y += (work[i+1].Y - work[i].Y) * u
		}
	}
	return work[0]
}
