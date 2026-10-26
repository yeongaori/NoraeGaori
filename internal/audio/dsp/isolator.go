package dsp

import "math"

const (
	isolatorLowCrossover  = 300.0
	isolatorHighCrossover = 4000.0
	butterworthQ          = math.Sqrt2 / 2
)

var eqTaper = []Point{
	{0.05, -26.4}, {0.1, -23.5}, {0.15, -15.4}, {0.2, -11.7}, {0.25, -8.2}, {0.3, -5.5},
	{0.35, -3.8}, {0.45, -1.75}, {0.5, 0}, {0.6, 1.15}, {0.75, 2.0}, {0.85, 3.2}, {0.95, 4.8}, {1.0, 6.2},
}

func EQGain(level float64) float64 {
	level = ClampUnit(level)
	first := eqTaper[0]
	if level < first.X {
		return level / first.X * decibelsToGain(first.Y)
	}
	return decibelsToGain(interpolate(eqTaper, level))
}

func decibelsToGain(decibels float64) float64 {
	return math.Pow(10, decibels/20)
}

func interpolate(knots []Point, x float64) float64 {
	if x <= knots[0].X {
		return knots[0].Y
	}
	for i := 1; i < len(knots); i++ {
		if x <= knots[i].X {
			previous := knots[i-1]
			span := knots[i].X - previous.X
			return previous.Y + (knots[i].Y-previous.Y)*(x-previous.X)/span
		}
	}
	return knots[len(knots)-1].Y
}

const (
	isolatorLowFirst = iota
	isolatorLowSecond
	isolatorLowAlign
	isolatorRestFirst
	isolatorRestSecond
	isolatorMidFirst
	isolatorMidSecond
	isolatorHighFirst
	isolatorHighSecond
	isolatorFilterCount
)

const (
	isolatorInput = iota
	isolatorLow
	isolatorRest
	isolatorMid
	isolatorHigh
	isolatorSignalCount
)

type isolatorRoute struct {
	from int
	to   int
}

var isolatorRoutes = [isolatorFilterCount]isolatorRoute{
	isolatorLowFirst:   {isolatorInput, isolatorLow},
	isolatorLowSecond:  {isolatorLow, isolatorLow},
	isolatorLowAlign:   {isolatorLow, isolatorLow},
	isolatorRestFirst:  {isolatorInput, isolatorRest},
	isolatorRestSecond: {isolatorRest, isolatorRest},
	isolatorMidFirst:   {isolatorRest, isolatorMid},
	isolatorMidSecond:  {isolatorMid, isolatorMid},
	isolatorHighFirst:  {isolatorRest, isolatorHigh},
	isolatorHighSecond: {isolatorHigh, isolatorHigh},
}

type Isolator struct {
	filters [isolatorFilterCount]Filter
	gains   [3]float64
	ready   bool
}

func (iso *Isolator) prepare(gains *[3]float64) {
	for stage := range isolatorLowAlign - isolatorLowFirst {
		iso.filters[isolatorLowFirst+stage].SetLowpass(isolatorLowCrossover, butterworthQ)
		iso.filters[isolatorRestFirst+stage].SetHighpass(isolatorLowCrossover, butterworthQ)
		iso.filters[isolatorMidFirst+stage].SetLowpass(isolatorHighCrossover, butterworthQ)
		iso.filters[isolatorHighFirst+stage].SetHighpass(isolatorHighCrossover, butterworthQ)
	}
	iso.filters[isolatorLowAlign].SetAllpass(isolatorHighCrossover, butterworthQ)
	iso.gains = *gains
	iso.ready = true
}

func (iso *Isolator) Process(buf []float64, levels *[3]float64) {
	var gains [3]float64
	for band, level := range levels {
		gains[band] = EQGain(level)
	}
	if !iso.ready {
		iso.prepare(&gains)
	}

	var coefficients [isolatorFilterCount]filterCoefficients
	var states [Channels][isolatorFilterCount]filterState
	for index := range iso.filters {
		coefficients[index] = iso.filters[index].filterCoefficients
		for channel := range states {
			states[channel][index] = iso.filters[index].states[channel]
		}
	}
	routes := isolatorRoutes
	steps := len(buf) / Channels
	from := iso.gains
	var work workBuffer
	for start := 0; start+1 < len(buf); start += workLength {
		chunk := buf[start:min(start+workLength, len(buf))]
		end := copy(work[:], chunk) &^ 1
		for channel := range states {
			for i := channel; i < end; i += Channels {
				blend := float64((start+i)/Channels+1) / float64(steps)
				lowGain := from[0] + (gains[0]-from[0])*blend
				midGain := from[1] + (gains[1]-from[1])*blend
				highGain := from[2] + (gains[2]-from[2])*blend
				signals := [isolatorSignalCount]float64{isolatorInput: work[i]}
				for index := range routes {
					route := routes[index]
					states[channel][index], signals[route.to] = coefficients[index].next(states[channel][index], signals[route.from])
				}
				work[i] = signals[isolatorLow]*lowGain + float64(signals[isolatorMid]*midGain) + signals[isolatorHigh]*highGain
			}
		}
		copy(chunk, work[:end])
	}
	for index := range iso.filters {
		for channel := range states {
			iso.filters[index].states[channel] = states[channel][index]
		}
	}
	iso.gains = gains
}
