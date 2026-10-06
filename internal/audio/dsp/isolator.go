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

type Isolator struct {
	lowBand  [2]Biquad
	restBand [2]Biquad
	midBand  [2]Biquad
	highBand [2]Biquad
	lowAlign Biquad
	low      []float64
	mid      []float64
	gains    [3]float64
	ready    bool
}

func (iso *Isolator) prepare(gains [3]float64) {
	for stage := range iso.lowBand {
		iso.lowBand[stage].SetLowpass(isolatorLowCrossover, butterworthQ)
		iso.restBand[stage].SetHighpass(isolatorLowCrossover, butterworthQ)
		iso.midBand[stage].SetLowpass(isolatorHighCrossover, butterworthQ)
		iso.highBand[stage].SetHighpass(isolatorHighCrossover, butterworthQ)
	}
	iso.lowAlign.SetAllpass(isolatorHighCrossover, butterworthQ)
	iso.gains = gains
	iso.ready = true
}

func (iso *Isolator) Process(buf []float64, levels [3]float64) {
	var gains [3]float64
	for band, level := range levels {
		gains[band] = EQGain(level)
	}
	if !iso.ready {
		iso.prepare(gains)
	}
	if cap(iso.low) < len(buf) {
		iso.low = make([]float64, len(buf))
		iso.mid = make([]float64, len(buf))
	}
	low := iso.low[:len(buf)]
	mid := iso.mid[:len(buf)]
	copy(low, buf)
	for stage := range iso.lowBand {
		iso.lowBand[stage].ProcessStereo(low)
		iso.restBand[stage].ProcessStereo(buf)
	}
	iso.lowAlign.ProcessStereo(low)
	copy(mid, buf)
	for stage := range iso.midBand {
		iso.midBand[stage].ProcessStereo(mid)
		iso.highBand[stage].ProcessStereo(buf)
	}

	steps := len(buf) / Channels
	for i := 0; i+1 < len(buf); i += Channels {
		blend := float64(i/Channels+1) / float64(steps)
		lowGain := iso.gains[0] + (gains[0]-iso.gains[0])*blend
		midGain := iso.gains[1] + (gains[1]-iso.gains[1])*blend
		highGain := iso.gains[2] + (gains[2]-iso.gains[2])*blend
		buf[i] = low[i]*lowGain + mid[i]*midGain + buf[i]*highGain
		buf[i+1] = low[i+1]*lowGain + mid[i+1]*midGain + buf[i+1]*highGain
	}
	iso.gains = gains
}
