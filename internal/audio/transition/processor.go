package transition

import "noraegaori/internal/audio/dsp"

type Processor struct {
	recipe          Recipe
	crossfadeFrames int
	beatFraction    float64
	frameStep       float64
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

	frameStep := 0.0
	if crossfadeFrames > 0 {
		frameStep = 1 / float64(crossfadeFrames)
	}

	processor := &Processor{
		recipe:          recipe,
		crossfadeFrames: crossfadeFrames,
		frameStep:       frameStep,
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
		return t.cutOutGain(progress), t.cutInGain(progress)
	}

	switch t.recipe.Volume {
	case VolumeOverlap:
		aGain := 1.0
		if progress > 1-beat {
			aGain = dsp.QSinOut((progress - (1 - beat)) / beat)
		}
		bGain := dsp.QSinIn(progress / beat)
		headroom := t.headroom(progress)
		return aGain * headroom, bGain * headroom
	case VolumeFadeInFadeOut:
		aGain := dsp.QSinOut(dsp.ClampUnit(progress / 0.45))
		bGain := dsp.QSinIn(dsp.ClampUnit((progress - 0.55) / 0.45))
		return aGain, bGain
	case VolumeCutInFadeOut:
		headroom := t.headroom(progress)
		return dsp.QSinOut(progress) * headroom, t.cutInGain(progress) * headroom
	case VolumeFadeInCutOut:
		headroom := t.headroom(progress)
		return t.cutOutGain(progress) * headroom, dsp.QSinIn(progress) * headroom
	}

	return dsp.QSinOut(progress), dsp.QSinIn(progress)
}

func (t *Processor) headroom(progress float64) float64 {
	beat := t.beatFraction
	duck := dsp.SmoothStep(progress/beat) * (1 - dsp.SmoothStep((progress-(1-beat))/beat))
	return 1 - (1-overlapHeadroom)*duck
}

func (t *Processor) cutWidth() float64 {
	return t.beatFraction * cutBeatFraction
}

func (t *Processor) cutInGain(progress float64) float64 {
	width := t.cutWidth()
	return dsp.RampAt(progress, width/2, width)
}

func (t *Processor) cutOutGain(progress float64) float64 {
	if t.hasDryCutEffect() {
		return 1
	}
	width := t.cutWidth()
	return 1 - dsp.RampAt(progress, 1-width/2, width)
}

func (t *Processor) hasDryCutEffect() bool {
	return t.recipe.Effect == EffectReverbCutEnd || t.recipe.Effect == EffectEchoHalfCutEnd
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
			highDB = EQCutDB * dsp.RampAt(progress, 0.25, 0.5)
			midDB = EQCutDB * dsp.RampAt(progress, 0.5, 0.6)
			lowDB = EQKillDB * dsp.RampAt(progress, 0.75, 0.5)
		} else {
			highDB = EQCutDB * (1 - dsp.RampAt(progress, 0.25, 0.5))
			midDB = EQCutDB * (1 - dsp.RampAt(progress, 0.5, 0.6))
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
			freq := dsp.SweepFrequency(filterOpenFreq, filterClosedFreq, easeIn(progress))
			if freq < filterOpenThreshold {
				t.aSweep.SetLowpass(freq, filterQ)
				t.aSweep.ProcessStereo(buf)
			}
		case FilterLowPassInHighPassOut:
			freq := dsp.SweepFrequency(highPassRestFreq, highPassPeakFreq, easeIn(progress))
			if freq > filterRestThreshold {
				t.aSweep.SetHighpass(freq, filterQ)
				t.aSweep.ProcessStereo(buf)
			}
		}
		return
	}

	switch t.recipe.Filter {
	case FilterLowPassIn, FilterLowPassInOut, FilterLowPassInHighPassOut:
		freq := dsp.SweepFrequency(filterClosedFreq, filterOpenFreq, easeOut(progress))
		if freq < filterOpenThreshold {
			t.bSweep.SetLowpass(freq, filterQ)
			t.bSweep.ProcessStereo(buf)
		}
	}
}

func easeIn(progress float64) float64 {
	progress = dsp.ClampUnit(progress)
	return progress * progress
}

func easeOut(progress float64) float64 {
	remaining := 1 - dsp.ClampUnit(progress)
	return 1 - remaining*remaining
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
	t.processBlocks(t.aBuffer, progress, true)
	return t.aBuffer
}

func (t *Processor) ProcessB(frame []int16, progress float64) []float64 {
	dsp.FrameToFloat(frame, t.bBuffer)
	t.processBlocks(t.bBuffer, progress, false)
	return t.bBuffer
}

func (t *Processor) processBlocks(buf []float64, progress float64, isA bool) {
	blockLength := paramBlockSamples * dsp.Channels
	blocks := (len(buf) + blockLength - 1) / blockLength
	for k := 0; k < blocks; k++ {
		block := buf[k*blockLength : min((k+1)*blockLength, len(buf))]
		blockProgress := progress + (float64(k)+0.5)/float64(blocks)*t.frameStep
		t.applyEQ(block, blockProgress, isA)
		t.applyFilter(block, blockProgress, isA)
		if isA {
			t.applyEffect(block, blockProgress)
		}
	}
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
	total     int
	gain      float64
	buffer    []float64
	limiter   dsp.Limiter
}

func (t *Processor) MakeTail(gain float64) *Tail {
	return t.makeTail(gain, ReverbTailFrames, EchoTailFrames)
}

func (t *Processor) MakeHandoffTail(gain float64) *Tail {
	return t.makeTail(gain, HandoffReverbTailFrames, HandoffEchoTailFrames)
}

func (t *Processor) makeTail(gain float64, reverbFrames, echoFrames int) *Tail {
	if !t.hasTail() {
		return nil
	}
	frames := reverbFrames
	if t.recipe.Effect == EffectEchoHalfCutEnd {
		frames = echoFrames
	}
	if t.tailBuffer == nil {
		t.tailBuffer = make([]float64, dsp.FrameSize*dsp.Channels)
	}
	return &Tail{
		processor: t,
		remaining: frames,
		total:     frames,
		gain:      gain,
		buffer:    t.tailBuffer,
	}
}

func (tail *Tail) Apply(frame []int16) bool {
	if tail == nil {
		return false
	}
	if tail.remaining > 0 {
		tail.fillRingOut()
	} else if tail.limiter.IsIdle() {
		return false
	} else {
		dsp.SilenceFloat(tail.buffer)
	}

	for i := range frame {
		tail.buffer[i] += float64(frame[i])
	}
	tail.limiter.ProcessStereo(tail.buffer, dsp.FullScale)
	dsp.FloatToFrame(tail.buffer, frame)
	return tail.remaining > 0 || !tail.limiter.IsIdle()
}

func (tail *Tail) fillRingOut() {
	dsp.SilenceFloat(tail.buffer)
	if tail.processor.recipe.Effect == EffectEchoHalfCutEnd {
		tail.processor.echoUnit.Dry = 0
		tail.processor.echoUnit.Wet = echoWet
		tail.processor.echoUnit.ProcessStereo(tail.buffer)
	} else {
		_, wet := tail.processor.effectMix(1)
		tail.processor.reverbUnit.ProcessStereo(tail.buffer, 0, wet)
	}

	scale := tail.gain * dsp.QSinOut(1-float64(tail.remaining)/float64(tail.total))
	for i := range tail.buffer {
		tail.buffer[i] *= scale
	}
	tail.remaining--
}
