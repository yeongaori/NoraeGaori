package dsp

import "math"

const (
	EchoFeedback   = 0.67
	echoLoopCutoff = 250.0
	echoReadMargin = 2
)

type Echo struct {
	left     []float64
	right    []float64
	index    int
	delay    float64
	target   float64
	feedback float64
	loop     Biquad
}

func PrepareEcho(delaySeconds, feedback float64) *Echo {
	delay := math.Max(1, math.Round(delaySeconds*SampleRate))
	size := int(delay) + echoReadMargin
	echo := &Echo{
		left:     make([]float64, size),
		right:    make([]float64, size),
		delay:    delay,
		target:   delay,
		feedback: feedback,
	}
	echo.loop.SetHighpass(echoLoopCutoff, butterworthQ)
	return echo
}

func (e *Echo) SetDelay(seconds float64) {
	e.target = math.Max(1, math.Min(seconds*SampleRate, float64(len(e.left)-echoReadMargin)))
}

func (e *Echo) Render(input, output []float64) {
	start := e.delay
	steps := len(input) / Channels
	for i := 0; i+1 < len(input); i += Channels {
		if e.target != start {
			e.delay = start + (e.target-start)*float64(i/Channels+1)/float64(steps)
		}
		delayedLeft, delayedRight := e.read()
		e.left[e.index] = e.loop.stepLeft(input[i] + delayedLeft*e.feedback)
		e.right[e.index] = e.loop.stepRight(input[i+1] + delayedRight*e.feedback)
		output[i] = delayedLeft
		output[i+1] = delayedRight
		e.index++
		if e.index == len(e.left) {
			e.index = 0
		}
	}
	e.delay = e.target
}

func (e *Echo) read() (float64, float64) {
	size := len(e.left)
	position := float64(e.index) - e.delay
	base := math.Floor(position)
	blend := position - base
	first := (int(base) + size) % size
	if blend == 0 {
		return e.left[first], e.right[first]
	}
	second := (first + 1) % size
	return e.left[first] + (e.left[second]-e.left[first])*blend,
		e.right[first] + (e.right[second]-e.right[first])*blend
}
