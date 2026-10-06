package dsp

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
	segment := c[0]
	for _, candidate := range c[1:] {
		if progress < candidate.Start {
			break
		}
		segment = candidate
	}
	local := 1.0
	if width := segment.End - segment.Start; width > 0 {
		local = ClampUnit((progress - segment.Start) / width)
	}
	return segment.valueAt(local)
}

func (s Segment) valueAt(x float64) float64 {
	switch len(s.Points) {
	case 0:
		return 0
	case 1:
		return s.Points[0].Y
	}
	if last := s.Points[len(s.Points)-1]; x >= last.X {
		return last.Y
	}
	if first := s.Points[0]; x <= first.X {
		return first.Y
	}

	low, high := 0.0, 1.0
	for step := 0; step < curveSolveSteps; step++ {
		middle := (low + high) / 2
		if bezierAt(s.Points, middle).X < x {
			low = middle
		} else {
			high = middle
		}
	}
	return bezierAt(s.Points, (low+high)/2).Y
}

func bezierAt(points []Point, u float64) Point {
	var inline [curveInlinePoints]Point
	work := inline[:0]
	if len(points) > curveInlinePoints {
		work = make([]Point, 0, len(points))
	}
	work = append(work, points...)
	for size := len(work) - 1; size > 0; size-- {
		for i := 0; i < size; i++ {
			work[i].X += (work[i+1].X - work[i].X) * u
			work[i].Y += (work[i+1].Y - work[i].Y) * u
		}
	}
	return work[0]
}
