package transition

import "noraegaori/internal/audio/dsp"

const (
	bandLow = iota
	bandMid
	bandHigh
	bandCount
)

const (
	bassFadeKnee        = 0.75
	switcharooBeats     = 2.0
	switcharooStageBeat = 4
)

var switcharooGates = []float64{0.25, 0.5, 1}

type deckCurves struct {
	volume dsp.Curve
	eq     [bandCount]dsp.Curve
	filter dsp.Curve
}

type segmentShape func(start, end, from, to float64) dsp.Segment

func flat(start, end, level float64) dsp.Segment {
	return dsp.Segment{Start: start, End: end, Points: []dsp.Point{{X: 0, Y: level}, {X: 1, Y: level}}}
}

func ramp(start, end, from, to float64) dsp.Segment {
	return dsp.Segment{Start: start, End: end, Points: []dsp.Point{{X: 0, Y: from}, {X: 1, Y: to}}}
}

func step(start, end, from, to float64) dsp.Segment {
	return kneeAt(start, end, from, to, 0.5)
}

func lateStep(start, end, from, to float64) dsp.Segment {
	return kneeAt(start, end, from, to, bassFadeKnee)
}

func kneeAt(start, end, from, to, knee float64) dsp.Segment {
	return dsp.Segment{Start: start, End: end, Points: []dsp.Point{
		{X: 0, Y: from}, {X: knee, Y: from}, {X: knee, Y: to}, {X: 1, Y: to},
	}}
}

func change(start, end, from, to float64, shape segmentShape) dsp.Curve {
	curve := dsp.Curve{}
	if start > 0 {
		curve = append(curve, flat(0, start, from))
	}
	curve = append(curve, shape(start, end, from, to))
	if end < 1 {
		curve = append(curve, flat(end, 1, to))
	}
	return curve
}

func quarterBeat(bars int) float64 {
	return 1 / float64(max(bars, minimumBars)*quarterBeatParts)
}

func tickBeat(bars int) float64 {
	return 1 / float64(max(bars, minimumBars)*tickBeatParts)
}

func lastBar(bars int) float64 {
	return 1 - 1/float64(max(bars, minimumBars))
}

func buildDeckCurves(side *Side, isOutgoing bool, bars int) deckCurves {
	return deckCurves{
		volume: volumeCurve(side.Volume, isOutgoing, bars),
		eq:     eqCurves(side.EQ, isOutgoing, bars),
		filter: filterCurve(side.Filter, isOutgoing),
	}
}

func volumeCurve(style VolumeStyle, isOutgoing bool, bars int) dsp.Curve {
	quarter := quarterBeat(bars)
	tick := tickBeat(bars)
	if isOutgoing {
		switch style {
		case VolumeCrossfade:
			return dsp.Curve{ramp(0, 1, 1, 0)}
		case VolumeSlow:
			return change(0.5, 1, 1, 0, ramp)
		case VolumeFast:
			return change(0.5, 0.5+quarter, 1, 0, ramp)
		case VolumeFastAtEdge:
			return change(1-tick, 1, 1, 0, step)
		case VolumeSemiFastAtEnd:
			return change(lastBar(bars), 1, 1, 0, ramp)
		case VolumeSwitcharoo:
			return switcharoo(bars, 0)
		}
		return dsp.Curve{kneeAt(0, 1, 1, 0, 0.6)}
	}
	switch style {
	case VolumeCrossfade:
		return dsp.Curve{ramp(0, 1, 0, 1)}
	case VolumeSlow:
		return change(0, 0.5, 0, 1, ramp)
	case VolumeFast:
		return change(0.5-quarter, 0.5, 0, 1, ramp)
	case VolumeFastAtEdge:
		return change(0, tick, 0, 1, step)
	case VolumeSwitcharoo:
		return switcharoo(bars, 1)
	}
	return dsp.Curve{kneeAt(0, 1, 0, 1, 0.4)}
}

func switcharoo(bars int, firstLevel float64) dsp.Curve {
	beats := float64(max(bars, minimumBars) * 4)
	var widths []float64
	remaining := beats
	for _, gate := range switcharooGates {
		span := min(remaining, switcharooStageBeat)
		for count := int(span / gate); count > 0; count-- {
			widths = append(widths, gate)
		}
		remaining -= span
	}
	for ; remaining > 0; remaining -= switcharooBeats {
		widths = append(widths, switcharooBeats)
	}

	curve := make(dsp.Curve, 0, len(widths))
	level := firstLevel
	position := 0.0
	for index := len(widths) - 1; index >= 0; index-- {
		end := position + widths[index]/beats
		if index == 0 {
			end = 1
		}
		curve = append(curve, flat(position, end, level))
		position = end
		level = 1 - level
	}
	return curve
}

type bandMove struct {
	band   int
	timing func(bars int) (float64, float64, segmentShape)
}

func centerSwap(bars int) (float64, float64, segmentShape) {
	return 0.5 - quarterBeat(bars), 0.5, step
}

func endSwap(bars int) (float64, float64, segmentShape) {
	return 1 - quarterBeat(bars), 1, step
}

func startSwap(bars int) (float64, float64, segmentShape) {
	return 0, tickBeat(bars), step
}

func oneBarFromEndSwap(bars int) (float64, float64, segmentShape) {
	return lastBar(bars), lastBar(bars) + quarterBeat(bars), step
}

func wholeStep(int) (float64, float64, segmentShape) {
	return 0, 1, step
}

func firstHalfStep(int) (float64, float64, segmentShape) {
	return 0, 0.5, step
}

func firstHalfLateStep(int) (float64, float64, segmentShape) {
	return 0, 0.5, lateStep
}

func wholeRamp(int) (float64, float64, segmentShape) {
	return 0, 1, ramp
}

var eqMoves = map[EQStyle][]bandMove{
	EQThreeBand:             {{bandLow, centerSwap}, {bandMid, wholeStep}, {bandHigh, firstHalfStep}},
	EQBassFade:              {{bandLow, firstHalfLateStep}},
	EQBassCrossfade:         {{bandLow, wholeRamp}},
	EQBassFast:              {{bandLow, centerSwap}},
	EQBassAndMidFast:        {{bandLow, centerSwap}, {bandMid, centerSwap}},
	EQMidFast:               {{bandMid, centerSwap}},
	EQHiFast:                {{bandHigh, centerSwap}},
	EQBassFastAtEnd:         {{bandLow, endSwap}},
	EQBassFastAtStart:       {{bandLow, startSwap}},
	EQBassFastOneBarFromEnd: {{bandLow, oneBarFromEndSwap}},
}

func eqCurves(style EQStyle, isOutgoing bool, bars int) [bandCount]dsp.Curve {
	var curves [bandCount]dsp.Curve
	for _, move := range eqMoves[style] {
		level := eqCut
		if move.band == bandLow {
			level = eqKill
		}
		start, end, shape := move.timing(bars)
		if isOutgoing {
			curves[move.band] = change(start, end, eqUnity, level, shape)
		} else {
			curves[move.band] = change(start, end, level, eqUnity, shape)
		}
	}
	return curves
}

func filterCurve(style FilterStyle, isOutgoing bool) dsp.Curve {
	edge := 0.0
	if style == FilterHighPass {
		edge = 1
	}
	switch {
	case style == FilterNone:
		return nil
	case isOutgoing:
		return change(0.5, 1, dsp.KnobCenter, edge, ramp)
	}
	return change(0, 0.5, edge, dsp.KnobCenter, ramp)
}

func noiseCurve(start float64, bars int) dsp.Curve {
	quarter := quarterBeat(bars) * (1 - start) * 2
	return dsp.Curve{
		flat(0, start, dsp.KnobCenter),
		ramp(start, start+quarter, dsp.KnobCenter, noiseColourStart),
		ramp(start+quarter, 1, noiseColourStart, noiseColourEnd),
	}
}
