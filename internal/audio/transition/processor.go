package transition

import "noraegaori/internal/audio/dsp"

type Processor struct {
	recipe          Recipe
	crossfadeFrames int
	beatFraction    float64
	periodSec       float64
	aBuffer         []float64
	bBuffer         []float64
	tailBuffer      []float64
	aLowShelf       dsp.Biquad
	aMidPeak        dsp.Biquad
	aHighShelf      dsp.Biquad
	bLowShelf       dsp.Biquad
	bMidPeak        dsp.Biquad
	bHighShelf      dsp.Biquad
	aSweep          dsp.Biquad
	bSweep          dsp.Biquad
	reverbUnit      *dsp.Reverb
	echoUnit        *dsp.DelayLine
	previousAGain   float64
	previousBGain   float64
	gainInitialized bool
	echoArmed       bool
	flatGains       bool
}

func NewProcessor(recipe Recipe, crossfadeFrames int, periodSec float64) *Processor {
	beatFraction := defaultBeatFraction
	if periodSec > 0 && crossfadeFrames > 0 {
		beatFraction = (periodSec * dsp.FramesPerSecond) / float64(crossfadeFrames)
	}
	if beatFraction < minBeatFraction {
		beatFraction = minBeatFraction
	}
	if beatFraction > maxBeatFraction {
		beatFraction = maxBeatFraction
	}

	processor := &Processor{
		recipe:          recipe,
		crossfadeFrames: crossfadeFrames,
		beatFraction:    beatFraction,
		periodSec:       periodSec,
		aBuffer:         make([]float64, dsp.FrameSize*dsp.Channels),
		bBuffer:         make([]float64, dsp.FrameSize*dsp.Channels),
	}
	processor.aLowShelf.SetBypass()
	processor.aMidPeak.SetBypass()
	processor.aHighShelf.SetBypass()
	processor.bLowShelf.SetBypass()
	processor.bMidPeak.SetBypass()
	processor.bHighShelf.SetBypass()
	processor.aSweep.SetBypass()
	processor.bSweep.SetBypass()

	switch recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		processor.reverbUnit = dsp.NewReverb()
	case EffectEchoHalfCutEnd:
		processor.echoUnit = dsp.NewDelayLine()
		delaySeconds := 0.25
		if periodSec > 0 {
			delaySeconds = periodSec / 2
		}
		processor.echoUnit.SetDelaySeconds(delaySeconds)
		processor.echoUnit.Feedback = echoFeedback
	}

	return processor
}

func (t *Processor) Gains(progress float64) (float64, float64) {
	progress = dsp.ClampUnit(progress)
	beat := t.beatFraction

	if t.flatGains && t.recipe.Volume == VolumeSmoothCrossfade {
		return 1, 1
	}

	switch t.recipe.Volume {
	case VolumeOverlap:
		aGain := 1.0
		if progress > 1-beat {
			aGain = dsp.QSinOut((progress - (1 - beat)) / beat)
		}
		bGain := dsp.QSinIn(progress / beat)
		return aGain * overlapHeadroom, bGain * overlapHeadroom
	case VolumeFadeInFadeOut:
		aGain := dsp.QSinOut(dsp.ClampUnit(progress / 0.45))
		bGain := dsp.QSinIn(dsp.ClampUnit((progress - 0.55) / 0.45))
		return aGain, bGain
	case VolumeCutInFadeOut:
		return dsp.QSinOut(progress) * overlapHeadroom, overlapHeadroom
	case VolumeFadeInCutOut:
		return overlapHeadroom, dsp.QSinIn(progress) * overlapHeadroom
	}

	return dsp.QSinOut(progress), dsp.QSinIn(progress)
}

func (t *Processor) eqGains(progress float64, isA bool) (float64, float64, float64) {
	beat := t.beatFraction
	var lowDB, midDB, highDB float64

	switch t.recipe.EQ {
	case EQCenterBassSwap:
		ramp := dsp.RampAt(progress, 0.5, beat)
		if isA {
			lowDB = EQKillDB * ramp
		} else {
			lowDB = EQKillDB * (1 - ramp)
		}
	case EQEndBassSwap:
		ramp := dsp.RampAt(progress, 1-beat, beat)
		if isA {
			lowDB = EQKillDB * ramp
		} else {
			lowDB = EQKillDB * (1 - ramp)
		}
	case EQStartBassSwap:
		ramp := dsp.RampAt(progress, beat, beat)
		if isA {
			lowDB = EQKillDB * ramp
		} else {
			lowDB = EQKillDB * (1 - ramp)
		}
	case EQThreeBandFade:
		if isA {
			highDB = EQKillDB * dsp.RampAt(progress, 0.25, 0.5)
			midDB = EQKillDB * dsp.RampAt(progress, 0.5, 0.6)
			lowDB = EQKillDB * dsp.RampAt(progress, 0.75, 0.5)
		} else {
			highDB = EQKillDB * (1 - dsp.RampAt(progress, 0.25, 0.5))
			midDB = EQKillDB * (1 - dsp.RampAt(progress, 0.5, 0.6))
			lowDB = EQKillDB * (1 - dsp.RampAt(progress, 0.75, 0.5))
		}
	case EQQuickBass:
		quick := beat * 0.35
		ramp := dsp.RampAt(progress, 0.5, quick)
		if isA {
			lowDB = EQKillDB * ramp
		} else {
			lowDB = EQKillDB*(1-ramp) + 3*dsp.BellAt(progress, 0.5+quick, beat)
		}
	}

	return lowDB, midDB, highDB
}

func (t *Processor) applyEQ(buf []float64, progress float64, isA bool) {
	if t.recipe.EQ == EQNone {
		return
	}
	lowDB, midDB, highDB := t.eqGains(progress, isA)

	lowShelf, midPeak, highShelf := &t.aLowShelf, &t.aMidPeak, &t.aHighShelf
	if !isA {
		lowShelf, midPeak, highShelf = &t.bLowShelf, &t.bMidPeak, &t.bHighShelf
	}

	if lowDB < -0.1 || lowDB > 0.1 {
		lowShelf.SetLowShelf(EQLowFreq, EQShelfQ, lowDB)
		lowShelf.ProcessStereo(buf)
	}
	if midDB < -0.1 || midDB > 0.1 {
		midPeak.SetPeaking(EQMidFreq, EQMidQ, midDB)
		midPeak.ProcessStereo(buf)
	}
	if highDB < -0.1 || highDB > 0.1 {
		highShelf.SetHighShelf(EQHighFreq, EQShelfQ, highDB)
		highShelf.ProcessStereo(buf)
	}
}

func (t *Processor) applyFilter(buf []float64, progress float64, isA bool) {
	if t.recipe.Filter == FilterNone {
		return
	}

	if isA {
		switch t.recipe.Filter {
		case FilterLowPassOut, FilterLowPassInOut:
			freq := dsp.SweepFrequency(filterOpenFreq, filterClosedFreq, progress)
			if freq < filterOpenThreshold {
				t.aSweep.SetLowpass(freq, filterQ)
				t.aSweep.ProcessStereo(buf)
			}
		case FilterLowPassInHighPassOut:
			freq := dsp.SweepFrequency(highPassRestFreq, highPassPeakFreq, progress)
			if freq > filterRestThreshold {
				t.aSweep.SetHighpass(freq, filterQ)
				t.aSweep.ProcessStereo(buf)
			}
		}
		return
	}

	switch t.recipe.Filter {
	case FilterLowPassIn, FilterLowPassInOut, FilterLowPassInHighPassOut:
		freq := dsp.SweepFrequency(filterClosedFreq, filterOpenFreq, progress)
		if freq < filterOpenThreshold {
			t.bSweep.SetLowpass(freq, filterQ)
			t.bSweep.ProcessStereo(buf)
		}
	}
}

func (t *Processor) effectMix(progress float64) (float64, float64) {
	beat := t.beatFraction

	switch t.recipe.Effect {
	case EffectReverbOutCenter:
		amount := dsp.SmoothStep((progress - 0.5) / 0.5)
		return 1 - 0.8*amount, reverbMaxWet * amount
	case EffectReverbOutEnd:
		amount := dsp.SmoothStep((progress - 0.75) / 0.25)
		return 1 - 0.7*amount, reverbMaxWet * amount
	case EffectReverbCutEnd:
		cut := dsp.RampAt(progress, 1-beat, beat*0.25)
		return 1 - cut, 0.6
	case EffectEchoHalfCutEnd:
		cut := dsp.RampAt(progress, 1-beat, beat*0.25)
		return 1 - cut, echoWet * cut
	}

	return 1, 0
}

func (t *Processor) applyEffect(buf []float64, progress float64) {
	switch t.recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		if t.reverbUnit == nil {
			return
		}
		dry, wet := t.effectMix(progress)
		if wet <= 0.0001 && dry >= 0.9999 {
			return
		}
		t.reverbUnit.ProcessStereo(buf, dry, wet)
	case EffectEchoHalfCutEnd:
		if t.echoUnit == nil {
			return
		}
		dry, wet := t.effectMix(progress)
		t.echoUnit.Dry = dry
		t.echoUnit.Wet = wet
		if wet > 0 {
			t.echoArmed = true
		}
		t.echoUnit.ProcessStereo(buf)
	}
}

func (t *Processor) ProcessA(frame []int16, progress float64) []float64 {
	dsp.FrameToFloat(frame, t.aBuffer)
	t.applyEQ(t.aBuffer, progress, true)
	t.applyFilter(t.aBuffer, progress, true)
	t.applyEffect(t.aBuffer, progress)
	return t.aBuffer
}

func (t *Processor) ProcessB(frame []int16, progress float64) []float64 {
	dsp.FrameToFloat(frame, t.bBuffer)
	t.applyEQ(t.bBuffer, progress, false)
	t.applyFilter(t.bBuffer, progress, false)
	return t.bBuffer
}

func (t *Processor) SetFlatGains(flat bool) {
	t.flatGains = flat
}

func (t *Processor) LastGain() float64 {
	return t.previousAGain
}

func (t *Processor) ApplyGainA(buf []float64, progress, volume float64) {
	aGain, _ := t.Gains(progress)
	aGain *= volume

	if !t.gainInitialized {
		t.previousAGain = aGain
		t.gainInitialized = true
	}

	dsp.ApplyGainRamp(buf, t.previousAGain, aGain)
	t.previousAGain = aGain
}

func (t *Processor) ApplyGains(aBuf, bBuf []float64, progress, volume float64) {
	aGain, bGain := t.Gains(progress)
	aGain *= volume
	bGain *= volume

	if !t.gainInitialized {
		t.previousAGain = aGain
		t.previousBGain = bGain
		t.gainInitialized = true
	}

	dsp.ApplyGainRamp(aBuf, t.previousAGain, aGain)
	dsp.ApplyGainRamp(bBuf, t.previousBGain, bGain)

	t.previousAGain = aGain
	t.previousBGain = bGain
}

func (t *Processor) hasTail() bool {
	switch t.recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		return t.reverbUnit != nil
	case EffectEchoHalfCutEnd:
		return t.echoUnit != nil && t.echoArmed
	}
	return false
}

type Tail struct {
	processor *Processor
	remaining int
	gain      float64
	buffer    []float64
}

func (t *Processor) MakeTail(gain float64) *Tail {
	if !t.hasTail() {
		return nil
	}
	frames := ReverbTailFrames
	if t.recipe.Effect == EffectEchoHalfCutEnd {
		frames = EchoTailFrames
	}
	if t.tailBuffer == nil {
		t.tailBuffer = make([]float64, dsp.FrameSize*dsp.Channels)
	}
	return &Tail{
		processor: t,
		remaining: frames,
		gain:      gain,
		buffer:    t.tailBuffer,
	}
}

func (tail *Tail) Apply(frame []int16) bool {
	if tail == nil || tail.remaining <= 0 {
		return false
	}

	dsp.SilenceFloat(tail.buffer)
	switch tail.processor.recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		_, wet := tail.processor.effectMix(1)
		if wet <= 0 {
			wet = 0.6
		}
		tail.processor.reverbUnit.ProcessStereo(tail.buffer, 0, wet)
	case EffectEchoHalfCutEnd:
		tail.processor.echoUnit.Dry = 0
		tail.processor.echoUnit.Wet = echoWet
		tail.processor.echoUnit.ProcessStereo(tail.buffer)
	default:
		tail.remaining = 0
		return false
	}

	decay := float64(tail.remaining) / float64(ReverbTailFrames)
	if tail.processor.recipe.Effect == EffectEchoHalfCutEnd {
		decay = float64(tail.remaining) / float64(EchoTailFrames)
	}
	scale := tail.gain * dsp.ClampUnit(decay)

	for i := range frame {
		sample := float64(frame[i]) + tail.buffer[i]*scale
		if sample > 32767 {
			frame[i] = 32767
		} else if sample < -32768 {
			frame[i] = -32768
		} else {
			frame[i] = int16(sample)
		}
	}

	tail.remaining--
	return tail.remaining > 0
}
