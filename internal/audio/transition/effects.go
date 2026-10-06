package transition

import (
	"math"

	"noraegaori/internal/audio/dsp"
)

const (
	fallbackPeriodSec = 0.5
	atEndStart        = 0.75
	crushPeak         = 1.0
	delayRampFloor    = 1.0 / 16
	barBeats          = 4
)

type effectKind int

const (
	effectReverb effectKind = iota + 1
	effectEcho
	effectDelay
	effectDelayRamp
	effectPhaser
	effectBitcrusher
	effectNoise
)

type effectSpec struct {
	kind        effectKind
	beats       float64
	ringsOut    bool
	buildsEarly bool
	atEnd       bool
}

var effectSpecs = map[FXStyle]effectSpec{
	FXReverbOutCenter:         {kind: effectReverb, ringsOut: true, buildsEarly: true},
	FXReverbCutEnd:            {kind: effectReverb},
	FXReverbOutEnd:            {kind: effectReverb, ringsOut: true},
	FXEchoHalfCutEnd:          {kind: effectEcho, beats: 0.5},
	FXEchoHalfOutEnd:          {kind: effectEcho, beats: 0.5, ringsOut: true},
	FXEchoThreeQuarterCutEnd:  {kind: effectEcho, beats: 0.75},
	FXEchoThreeQuarterOutEnd:  {kind: effectEcho, beats: 0.75, ringsOut: true},
	FXEchoBeatCutEnd:          {kind: effectEcho, beats: 1},
	FXEchoBeatOutEnd:          {kind: effectEcho, beats: 1, ringsOut: true},
	FXDelayHalfCutEnd:         {kind: effectDelay, beats: 0.5},
	FXDelayThreeQuarterCutEnd: {kind: effectDelay, beats: 0.75},
	FXNoise:                   {kind: effectNoise},
	FXNoiseAtEnd:              {kind: effectNoise, atEnd: true},
	FXDelayRamp:               {kind: effectDelayRamp, beats: 1},
	FXDelayRampAtEnd:          {kind: effectDelayRamp, beats: 1, atEnd: true},
	FXDelayOneBar:             {kind: effectDelay, beats: barBeats, ringsOut: true},
	FXDelayOneBarAtEnd:        {kind: effectDelay, beats: barBeats, ringsOut: true, atEnd: true},
	FXPhaser:                  {kind: effectPhaser},
	FXBitcrusher:              {kind: effectBitcrusher},
}

type sendEffect interface {
	Render(input, output []float64)
}

type insertEffect interface {
	Process(block []float64, from, to float64)
}

type deckEffect struct {
	spec      effectSpec
	amount    dsp.Curve
	send      sendEffect
	glide     *dsp.Echo
	insert    insertEffect
	riser     *dsp.NoiseRiser
	rampStart float64
	rampSec   float64
	dryGain   float64
	wetGain   float64
	level     float64
	primed    bool
}

func beatPeriod(periodSec float64) float64 {
	if periodSec > 0 {
		return periodSec
	}
	return fallbackPeriodSec
}

func prepareEffect(style FXStyle, isOutgoing bool, window *Window) *deckEffect {
	spec, ok := effectSpecs[style]
	if !ok {
		return nil
	}
	period := beatPeriod(window.PeriodSec)
	effect := &deckEffect{spec: spec}
	switch spec.kind {
	case effectNoise:
		if !isOutgoing {
			return nil
		}
		effect.amount = noiseCurve(spec.windowStart(), window.Bars)
		effect.riser = &dsp.NoiseRiser{}
		return effect
	case effectReverb:
		effect.send = dsp.PrepareReverb()
	case effectEcho:
		effect.send = dsp.PrepareEcho(spec.beats*period, dsp.EchoFeedback)
	case effectDelay:
		effect.send = dsp.PrepareEcho(spec.beats*period, 0)
	case effectDelayRamp:
		effect.glide = dsp.PrepareEcho(spec.beats*period, dsp.EchoFeedback)
		effect.send = effect.glide
		effect.rampStart = spec.windowStart()
		effect.rampSec = spec.beats * period
	case effectPhaser:
		effect.insert = dsp.PreparePhaser(1 / (barBeats * period))
	case effectBitcrusher:
		effect.insert = &dsp.Bitcrusher{}
	}
	peak := wetPeak
	if spec.kind == effectBitcrusher {
		peak = crushPeak
	}
	effect.amount = spec.amountCurve(isOutgoing, peak)
	return effect
}

func (s effectSpec) windowStart() float64 {
	if s.atEnd {
		return atEndStart
	}
	return 0.5
}

func (s effectSpec) amountCurve(isOutgoing bool, peak float64) dsp.Curve {
	if !isOutgoing {
		if s.atEnd {
			return cutAtEnd(0.5, peak)
		}
		return change(0, 0.5, peak, 0, ramp)
	}
	if s.buildsEarly {
		return change(0, 0.5, 0, peak, ramp)
	}
	if s.ringsOut {
		return change(s.windowStart(), 1, 0, peak, ramp)
	}
	return cutAtEnd(s.windowStart(), peak)
}

func cutAtEnd(start, peak float64) dsp.Curve {
	return dsp.Curve{flat(0, start, 0), ramp(start, wetCutEnd, 0, peak), flat(wetCutEnd, 1, 0)}
}

func (e *deckEffect) apply(block, scratch []float64, start, middle, end, volume float64) {
	switch {
	case e.riser != nil:
		e.addNoise(block, scratch, middle, volume)
	case e.insert != nil:
		if !e.primed {
			e.level = e.amount.At(start)
			e.primed = true
		}
		level := e.amount.At(end)
		e.insert.Process(block, e.level, level)
		e.level = level
	default:
		e.applySend(block, scratch, start, end)
	}
}

func (e *deckEffect) applySend(block, wetOut []float64, start, end float64) {
	if !e.primed {
		e.dryGain, e.wetGain = dsp.DryWet(e.amount.At(start))
		e.primed = true
	}
	if e.glide != nil {
		e.glide.SetDelay(e.delayAt(end))
	}
	e.send.Render(block, wetOut)
	dry, wet := dsp.DryWet(e.amount.At(end))
	steps := len(block) / dsp.Channels
	for i := 0; i+1 < len(block); i += dsp.Channels {
		blend := float64(i/dsp.Channels+1) / float64(steps)
		dryGain := e.dryGain + (dry-e.dryGain)*blend
		wetGain := e.wetGain + (wet-e.wetGain)*blend
		block[i] = block[i]*dryGain + wetOut[i]*wetGain
		block[i+1] = block[i+1]*dryGain + wetOut[i+1]*wetGain
	}
	e.dryGain, e.wetGain = dry, wet
}

func (e *deckEffect) delayAt(progress float64) float64 {
	travel := math.Max(0, math.Min(1, (progress-e.rampStart)/(1-e.rampStart)))
	return e.rampSec * math.Pow(delayRampFloor, travel)
}

func (e *deckEffect) addNoise(block, noise []float64, middle, volume float64) {
	e.riser.Render(noise, e.amount.At(middle))
	for i := range block {
		block[i] += noise[i] * volume
	}
}

func (e *deckEffect) ringsOut() bool {
	return e != nil && e.send != nil && e.spec.ringsOut
}

type jogSpec struct {
	beats      float64
	end        float64
	isSpinback bool
}

var jogSpecs = map[LoopStyle]jogSpec{
	LoopSpinbackOneBeat:      {beats: 1, end: 1, isSpinback: true},
	LoopSpinbackTwoBeats:     {beats: 2, end: 1, isSpinback: true},
	LoopSpinbackFourBeats:    {beats: 4, end: 1, isSpinback: true},
	LoopVinylStopCenter:      {beats: 4, end: 0.5},
	LoopVinylStopCenterShort: {beats: 2, end: 0.5},
	LoopVinylStopEnd:         {beats: 4, end: 1},
	LoopVinylStopEndShort:    {beats: 2, end: 1},
}

func prepareJogwheel(loop LoopStyle, window *Window) *Jogwheel {
	spec, ok := jogSpecs[loop]
	if !ok {
		return nil
	}
	if spec.isSpinback {
		return startSpinback(spec.beats, spec.end, window)
	}
	return startVinylStop(spec.beats, spec.end, window)
}
