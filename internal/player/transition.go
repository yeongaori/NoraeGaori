package player

import (
	"fmt"
	"math"
)

type VolumeStyle int

const (
	VolumeSmoothCrossfade VolumeStyle = iota
	VolumeOverlap
	VolumeFadeInFadeOut
	VolumeCutInFadeOut
	VolumeFadeInCutOut
)

type EQStyle int

const (
	EQNone EQStyle = iota
	EQCenterBassSwap
	EQEndBassSwap
	EQStartBassSwap
	EQThreeBandFade
	EQQuickBass
)

type FilterStyle int

const (
	FilterNone FilterStyle = iota
	FilterLowPassOut
	FilterLowPassIn
	FilterLowPassInOut
	FilterLowPassInHighPassOut
)

type EffectStyle int

const (
	EffectNone EffectStyle = iota
	EffectReverbOutCenter
	EffectReverbCutEnd
	EffectReverbOutEnd
	EffectEchoHalfCutEnd
)

type LoopStyle int

const (
	LoopNone LoopStyle = iota
	LoopOneBeat
	LoopTwoBeats
	LoopFourBeats
	LoopEightBeats
)

const (
	styleAuto           = "auto"
	eqKillDB            = -40.0
	eqLowFreq           = 250.0
	eqMidFreq           = 1000.0
	eqHighFreq          = 4000.0
	eqShelfQ            = 0.707
	eqMidQ              = 0.9
	filterQ             = 0.9
	filterOpenFreq      = 20000.0
	filterClosedFreq    = 200.0
	highPassRestFreq    = 25.0
	highPassPeakFreq    = 3000.0
	filterOpenThreshold = 18000.0
	filterRestThreshold = 30.0
	overlapHeadroom     = 0.85
	reverbTailFrames    = 140
	echoTailFrames      = 170
	echoFeedback        = 0.55
	echoWet             = 0.85
	reverbMaxWet        = 0.9
	minBeatFraction     = 0.02
	maxBeatFraction     = 0.5
	defaultBeatFraction = 0.1
	keyConfidenceFloor  = 0.04
	bpmMatchTolerance   = 0.03
	bpmLooseTolerance   = 0.08
	tempoFoldTolerance  = 0.12
)

var volumeStyleNames = map[string]VolumeStyle{
	"smooth":         VolumeSmoothCrossfade,
	"overlap":        VolumeOverlap,
	"fadein_fadeout": VolumeFadeInFadeOut,
	"cutin_fadeout":  VolumeCutInFadeOut,
	"fadein_cutout":  VolumeFadeInCutOut,
}

var eqStyleNames = map[string]EQStyle{
	"none":             EQNone,
	"center_bass_swap": EQCenterBassSwap,
	"end_bass_swap":    EQEndBassSwap,
	"start_bass_swap":  EQStartBassSwap,
	"three_band_fade":  EQThreeBandFade,
	"quick_bass":       EQQuickBass,
}

var filterStyleNames = map[string]FilterStyle{
	"none":                    FilterNone,
	"lowpass_out":             FilterLowPassOut,
	"lowpass_in":              FilterLowPassIn,
	"lowpass_in_out":          FilterLowPassInOut,
	"lowpass_in_highpass_out": FilterLowPassInHighPassOut,
}

var effectStyleNames = map[string]EffectStyle{
	"none":              EffectNone,
	"reverb_out_center": EffectReverbOutCenter,
	"reverb_cut_end":    EffectReverbCutEnd,
	"reverb_out_end":    EffectReverbOutEnd,
	"echo_half_cut_end": EffectEchoHalfCutEnd,
}

var loopStyleNames = map[string]LoopStyle{
	"none":        LoopNone,
	"one_beat":    LoopOneBeat,
	"two_beats":   LoopTwoBeats,
	"four_beats":  LoopFourBeats,
	"eight_beats": LoopEightBeats,
}

type TransitionRecipe struct {
	Volume VolumeStyle
	EQ     EQStyle
	Filter FilterStyle
	Effect EffectStyle
	Loop   LoopStyle
}

type TransitionStyleOverrides struct {
	Volume string
	EQ     string
	Filter string
	Effect string
	Loop   string
}

func defaultTransitionRecipe() TransitionRecipe {
	return TransitionRecipe{
		Volume: VolumeSmoothCrossfade,
		EQ:     EQNone,
		Filter: FilterNone,
		Effect: EffectNone,
		Loop:   LoopNone,
	}
}

func (s VolumeStyle) String() string {
	return lookupStyleName(volumeStyleNames, s)
}

func (s EQStyle) String() string {
	return lookupStyleName(eqStyleNames, s)
}

func (s FilterStyle) String() string {
	return lookupStyleName(filterStyleNames, s)
}

func (s EffectStyle) String() string {
	return lookupStyleName(effectStyleNames, s)
}

func (s LoopStyle) String() string {
	return lookupStyleName(loopStyleNames, s)
}

func lookupStyleName[T comparable](names map[string]T, value T) string {
	for name, candidate := range names {
		if candidate == value {
			return name
		}
	}
	return styleAuto
}

func (r TransitionRecipe) String() string {
	return fmt.Sprintf("volume=%s eq=%s filter=%s effect=%s loop=%s",
		r.Volume, r.EQ, r.Filter, r.Effect, r.Loop)
}

var transitionStyleOrder = map[string][]string{
	"volume": {"smooth", "overlap", "fadein_fadeout", "cutin_fadeout", "fadein_cutout"},
	"eq":     {"none", "center_bass_swap", "end_bass_swap", "start_bass_swap", "three_band_fade", "quick_bass"},
	"filter": {"none", "lowpass_out", "lowpass_in", "lowpass_in_out", "lowpass_in_highpass_out"},
	"effect": {"none", "reverb_out_center", "reverb_cut_end", "reverb_out_end", "echo_half_cut_end"},
	"loop":   {"none", "one_beat", "two_beats", "four_beats", "eight_beats"},
}

func TransitionStyleValues(category string) []string {
	values, ok := transitionStyleOrder[category]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values)+1)
	out = append(out, styleAuto)
	out = append(out, values...)
	return out
}

func ValidTransitionStyle(category, value string) bool {
	if value == styleAuto {
		return true
	}
	switch category {
	case "volume":
		_, ok := volumeStyleNames[value]
		return ok
	case "eq":
		_, ok := eqStyleNames[value]
		return ok
	case "filter":
		_, ok := filterStyleNames[value]
		return ok
	case "effect":
		_, ok := effectStyleNames[value]
		return ok
	case "loop":
		_, ok := loopStyleNames[value]
		return ok
	}
	return false
}

func recipeStyleMap(recipe TransitionRecipe) map[string]string {
	return map[string]string{
		"volume": recipe.Volume.String(),
		"eq":     recipe.EQ.String(),
		"filter": recipe.Filter.String(),
		"effect": recipe.Effect.String(),
		"loop":   recipe.Loop.String(),
	}
}

func overrideValue(overrides TransitionStyleOverrides, category string) string {
	switch category {
	case "volume":
		return overrides.Volume
	case "eq":
		return overrides.EQ
	case "filter":
		return overrides.Filter
	case "effect":
		return overrides.Effect
	case "loop":
		return overrides.Loop
	}
	return ""
}

func AutoTransitionStyles(a, b *TrackAnalysis) map[string]string {
	return recipeStyleMap(selectTransitionRecipe(a, b))
}

func TransitionCrossfadeFrames(autoMix bool, autoMixBeats int, crossfadeSec float64, a *TrackAnalysis) (int, float64) {
	effectiveSec := fallbackCrossfadeSec
	if crossfadeSec > 0 {
		effectiveSec = crossfadeSec
	}
	if autoMix && a != nil {
		effectiveSec = float64(autoMixBeats) * a.PeriodSec
		if effectiveSec < crossfadeMinSec {
			effectiveSec = crossfadeMinSec
		}
		if effectiveSec > crossfadeMaxSec {
			effectiveSec = crossfadeMaxSec
		}
	}
	return int(effectiveSec * framesPerSecond), effectiveSec
}

func ClampLoopStyle(loop LoopStyle, periodSec float64, crossfadeFrames int) (LoopStyle, int) {
	beats := loopBeatCount(loop)
	if beats <= 0 || periodSec <= 0 {
		return LoopNone, 0
	}
	frames := int(math.Round(float64(beats) * periodSec * framesPerSecond))
	if frames < 1 || frames*2 > crossfadeFrames {
		return LoopNone, 0
	}
	return loop, frames
}

func layerTransitionStyles(base TransitionRecipe, guild, song TransitionStyleOverrides) (TransitionRecipe, map[string]string, map[string]string) {
	recipe := base
	source := make(map[string]string, len(transitionStyleOrder))
	for category := range transitionStyleOrder {
		source[category] = "auto"
	}

	for _, layer := range []struct {
		name      string
		overrides TransitionStyleOverrides
	}{{"guild", guild}, {"song", song}} {
		for category := range transitionStyleOrder {
			value := overrideValue(layer.overrides, category)
			if value == "" || value == styleAuto || !ValidTransitionStyle(category, value) {
				continue
			}
			source[category] = layer.name
		}
		recipe = applyStyleOverrides(recipe, layer.overrides)
	}

	return recipe, recipeStyleMap(recipe), source
}

func ResolveTransitionStyles(a, b *TrackAnalysis, autoSelect bool, guild, song TransitionStyleOverrides) (TransitionRecipe, map[string]string, map[string]string) {
	base := defaultTransitionRecipe()
	if autoSelect {
		base = selectTransitionRecipe(a, b)
	}
	return layerTransitionStyles(base, guild, song)
}

func ResolveOutroStyles(a *TrackAnalysis, autoSelect bool, guild, song TransitionStyleOverrides) (TransitionRecipe, map[string]string, map[string]string) {
	base := defaultTransitionRecipe()
	if autoSelect {
		base = selectOutroRecipe(a)
	}
	return layerTransitionStyles(base, guild, song)
}

func AutoOutroStyles(a *TrackAnalysis) map[string]string {
	return recipeStyleMap(selectOutroRecipe(a))
}

func applyStyleOverrides(recipe TransitionRecipe, overrides TransitionStyleOverrides) TransitionRecipe {
	if style, ok := volumeStyleNames[overrides.Volume]; ok {
		recipe.Volume = style
	}
	if style, ok := eqStyleNames[overrides.EQ]; ok {
		recipe.EQ = style
	}
	if style, ok := filterStyleNames[overrides.Filter]; ok {
		recipe.Filter = style
	}
	if style, ok := effectStyleNames[overrides.Effect]; ok {
		recipe.Effect = style
	}
	if style, ok := loopStyleNames[overrides.Loop]; ok {
		recipe.Loop = style
	}
	return recipe
}

func loopBeatCount(style LoopStyle) int {
	switch style {
	case LoopOneBeat:
		return 1
	case LoopTwoBeats:
		return 2
	case LoopFourBeats:
		return 4
	case LoopEightBeats:
		return 8
	}
	return 0
}

func tempoDeltaFactor(bpmA, bpmB float64) (float64, float64) {
	if bpmA <= 0 || bpmB <= 0 {
		return 0, 1
	}
	best := math.Abs(bpmB-bpmA) / bpmA
	bestFactor := 1.0
	for _, factor := range []float64{0.5, 2} {
		delta := math.Abs(bpmB*factor-bpmA) / bpmA
		if delta < best && delta <= tempoFoldTolerance {
			best = delta
			bestFactor = factor
		}
	}
	return best, bestFactor
}

func tempoDelta(bpmA, bpmB float64) float64 {
	delta, _ := tempoDeltaFactor(bpmA, bpmB)
	return delta
}

func signedTempoDelta(bpmA, bpmB float64) float64 {
	if bpmA <= 0 || bpmB <= 0 {
		return 0
	}
	best := (bpmB - bpmA) / bpmA
	for _, factor := range []float64{0.5, 2} {
		if delta := (bpmB*factor - bpmA) / bpmA; math.Abs(delta) < math.Abs(best) {
			best = delta
		}
	}
	return best
}

func selectOutroRecipe(a *TrackAnalysis) TransitionRecipe {
	recipe := defaultTransitionRecipe()
	recipe.Filter = FilterLowPassOut

	if a == nil || a.BPM <= 0 {
		recipe.Effect = EffectReverbCutEnd
		return recipe
	}
	if a.PeriodSec > 0 {
		recipe.EQ = EQEndBassSwap
		recipe.Effect = EffectEchoHalfCutEnd
		return recipe
	}

	recipe.Effect = EffectReverbOutEnd
	return recipe
}

func selectTransitionRecipe(a, b *TrackAnalysis) TransitionRecipe {
	recipe := defaultTransitionRecipe()
	if a == nil || b == nil || a.BPM <= 0 || b.BPM <= 0 {
		return recipe
	}

	bpmDelta := tempoDelta(a.BPM, b.BPM)
	gridSolid := a.PeriodSec > 0 && b.PeriodSec > 0
	distance := camelotDistance(a, b)
	harmonic := distance >= 0 && distance <= 1

	switch {
	case bpmDelta < bpmMatchTolerance && harmonic:
		recipe.Volume = VolumeOverlap
		recipe.EQ = EQThreeBandFade
	case bpmDelta < bpmMatchTolerance:
		recipe.Volume = VolumeSmoothCrossfade
		recipe.EQ = EQCenterBassSwap
		recipe.Filter = FilterLowPassInHighPassOut
	case bpmDelta < bpmLooseTolerance && harmonic:
		recipe.Volume = VolumeSmoothCrossfade
		recipe.EQ = EQCenterBassSwap
		recipe.Filter = FilterLowPassIn
	case bpmDelta < bpmLooseTolerance:
		recipe.Volume = VolumeSmoothCrossfade
		recipe.EQ = EQEndBassSwap
		recipe.Filter = FilterLowPassInOut
		recipe.Effect = EffectReverbOutEnd
	case gridSolid:
		recipe.Volume = VolumeFadeInCutOut
		recipe.EQ = EQStartBassSwap
		recipe.Filter = FilterLowPassOut
		recipe.Effect = EffectEchoHalfCutEnd
		recipe.Loop = LoopFourBeats
	default:
		recipe.Volume = VolumeFadeInCutOut
		recipe.Filter = FilterLowPassOut
		recipe.Effect = EffectReverbCutEnd
	}

	return recipe
}

type transitionProcessor struct {
	recipe          TransitionRecipe
	crossfadeFrames int
	beatFraction    float64
	periodSec       float64
	aBuffer         []float64
	bBuffer         []float64
	tailBuffer      []float64
	aLowShelf       biquad
	aMidPeak        biquad
	aHighShelf      biquad
	bLowShelf       biquad
	bMidPeak        biquad
	bHighShelf      biquad
	aSweep          biquad
	bSweep          biquad
	reverbUnit      *reverb
	echoUnit        *delayLine
	previousAGain   float64
	previousBGain   float64
	gainInitialized bool
	echoArmed       bool
	flatGains       bool
}

func newTransitionProcessor(recipe TransitionRecipe, crossfadeFrames int, periodSec float64) *transitionProcessor {
	beatFraction := defaultBeatFraction
	if periodSec > 0 && crossfadeFrames > 0 {
		beatFraction = (periodSec * framesPerSecond) / float64(crossfadeFrames)
	}
	if beatFraction < minBeatFraction {
		beatFraction = minBeatFraction
	}
	if beatFraction > maxBeatFraction {
		beatFraction = maxBeatFraction
	}

	processor := &transitionProcessor{
		recipe:          recipe,
		crossfadeFrames: crossfadeFrames,
		beatFraction:    beatFraction,
		periodSec:       periodSec,
		aBuffer:         make([]float64, frameSize*channels),
		bBuffer:         make([]float64, frameSize*channels),
	}
	processor.aLowShelf.setBypass()
	processor.aMidPeak.setBypass()
	processor.aHighShelf.setBypass()
	processor.bLowShelf.setBypass()
	processor.bMidPeak.setBypass()
	processor.bHighShelf.setBypass()
	processor.aSweep.setBypass()
	processor.bSweep.setBypass()

	switch recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		processor.reverbUnit = newReverb()
	case EffectEchoHalfCutEnd:
		processor.echoUnit = newDelayLine()
		delaySeconds := 0.25
		if periodSec > 0 {
			delaySeconds = periodSec / 2
		}
		processor.echoUnit.setDelaySeconds(delaySeconds)
		processor.echoUnit.feedback = echoFeedback
	}

	return processor
}

func (t *transitionProcessor) gains(progress float64) (float64, float64) {
	progress = clampUnit(progress)
	beat := t.beatFraction

	if t.flatGains && t.recipe.Volume == VolumeSmoothCrossfade {
		return 1, 1
	}

	switch t.recipe.Volume {
	case VolumeOverlap:
		aGain := 1.0
		if progress > 1-beat {
			aGain = qsinOut((progress - (1 - beat)) / beat)
		}
		bGain := qsinIn(progress / beat)
		return aGain * overlapHeadroom, bGain * overlapHeadroom
	case VolumeFadeInFadeOut:
		aGain := qsinOut(clampUnit(progress / 0.45))
		bGain := qsinIn(clampUnit((progress - 0.55) / 0.45))
		return aGain, bGain
	case VolumeCutInFadeOut:
		return qsinOut(progress) * overlapHeadroom, overlapHeadroom
	case VolumeFadeInCutOut:
		return overlapHeadroom, qsinIn(progress) * overlapHeadroom
	}

	return qsinOut(progress), qsinIn(progress)
}

func (t *transitionProcessor) eqGains(progress float64, isA bool) (float64, float64, float64) {
	beat := t.beatFraction
	var lowDB, midDB, highDB float64

	switch t.recipe.EQ {
	case EQCenterBassSwap:
		ramp := rampAt(progress, 0.5, beat)
		if isA {
			lowDB = eqKillDB * ramp
		} else {
			lowDB = eqKillDB * (1 - ramp)
		}
	case EQEndBassSwap:
		ramp := rampAt(progress, 1-beat, beat)
		if isA {
			lowDB = eqKillDB * ramp
		} else {
			lowDB = eqKillDB * (1 - ramp)
		}
	case EQStartBassSwap:
		ramp := rampAt(progress, beat, beat)
		if isA {
			lowDB = eqKillDB * ramp
		} else {
			lowDB = eqKillDB * (1 - ramp)
		}
	case EQThreeBandFade:
		if isA {
			highDB = eqKillDB * rampAt(progress, 0.25, 0.5)
			midDB = eqKillDB * rampAt(progress, 0.5, 0.6)
			lowDB = eqKillDB * rampAt(progress, 0.75, 0.5)
		} else {
			highDB = eqKillDB * (1 - rampAt(progress, 0.25, 0.5))
			midDB = eqKillDB * (1 - rampAt(progress, 0.5, 0.6))
			lowDB = eqKillDB * (1 - rampAt(progress, 0.75, 0.5))
		}
	case EQQuickBass:
		quick := beat * 0.35
		ramp := rampAt(progress, 0.5, quick)
		if isA {
			lowDB = eqKillDB * ramp
		} else {
			lowDB = eqKillDB*(1-ramp) + 3*bellAt(progress, 0.5+quick, beat)
		}
	}

	return lowDB, midDB, highDB
}

func (t *transitionProcessor) applyEQ(buf []float64, progress float64, isA bool) {
	if t.recipe.EQ == EQNone {
		return
	}
	lowDB, midDB, highDB := t.eqGains(progress, isA)

	lowShelf, midPeak, highShelf := &t.aLowShelf, &t.aMidPeak, &t.aHighShelf
	if !isA {
		lowShelf, midPeak, highShelf = &t.bLowShelf, &t.bMidPeak, &t.bHighShelf
	}

	if lowDB < -0.1 || lowDB > 0.1 {
		lowShelf.setLowShelf(eqLowFreq, eqShelfQ, lowDB)
		lowShelf.processStereo(buf)
	}
	if midDB < -0.1 || midDB > 0.1 {
		midPeak.setPeaking(eqMidFreq, eqMidQ, midDB)
		midPeak.processStereo(buf)
	}
	if highDB < -0.1 || highDB > 0.1 {
		highShelf.setHighShelf(eqHighFreq, eqShelfQ, highDB)
		highShelf.processStereo(buf)
	}
}

func (t *transitionProcessor) applyFilter(buf []float64, progress float64, isA bool) {
	if t.recipe.Filter == FilterNone {
		return
	}

	if isA {
		switch t.recipe.Filter {
		case FilterLowPassOut, FilterLowPassInOut:
			freq := sweepFrequency(filterOpenFreq, filterClosedFreq, progress)
			if freq < filterOpenThreshold {
				t.aSweep.setLowpass(freq, filterQ)
				t.aSweep.processStereo(buf)
			}
		case FilterLowPassInHighPassOut:
			freq := sweepFrequency(highPassRestFreq, highPassPeakFreq, progress)
			if freq > filterRestThreshold {
				t.aSweep.setHighpass(freq, filterQ)
				t.aSweep.processStereo(buf)
			}
		}
		return
	}

	switch t.recipe.Filter {
	case FilterLowPassIn, FilterLowPassInOut, FilterLowPassInHighPassOut:
		freq := sweepFrequency(filterClosedFreq, filterOpenFreq, progress)
		if freq < filterOpenThreshold {
			t.bSweep.setLowpass(freq, filterQ)
			t.bSweep.processStereo(buf)
		}
	}
}

func (t *transitionProcessor) effectMix(progress float64) (float64, float64) {
	beat := t.beatFraction

	switch t.recipe.Effect {
	case EffectReverbOutCenter:
		amount := smoothStep((progress - 0.5) / 0.5)
		return 1 - 0.8*amount, reverbMaxWet * amount
	case EffectReverbOutEnd:
		amount := smoothStep((progress - 0.75) / 0.25)
		return 1 - 0.7*amount, reverbMaxWet * amount
	case EffectReverbCutEnd:
		cut := rampAt(progress, 1-beat, beat*0.25)
		return 1 - cut, 0.6
	case EffectEchoHalfCutEnd:
		cut := rampAt(progress, 1-beat, beat*0.25)
		return 1 - cut, echoWet * cut
	}

	return 1, 0
}

func (t *transitionProcessor) applyEffect(buf []float64, progress float64) {
	switch t.recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		if t.reverbUnit == nil {
			return
		}
		dry, wet := t.effectMix(progress)
		if wet <= 0.0001 && dry >= 0.9999 {
			return
		}
		t.reverbUnit.processStereo(buf, dry, wet)
	case EffectEchoHalfCutEnd:
		if t.echoUnit == nil {
			return
		}
		dry, wet := t.effectMix(progress)
		t.echoUnit.dry = dry
		t.echoUnit.wet = wet
		if wet > 0 {
			t.echoArmed = true
		}
		t.echoUnit.processStereo(buf)
	}
}

func (t *transitionProcessor) processA(frame []int16, progress float64) []float64 {
	frameToFloat(frame, t.aBuffer)
	t.applyEQ(t.aBuffer, progress, true)
	t.applyFilter(t.aBuffer, progress, true)
	t.applyEffect(t.aBuffer, progress)
	return t.aBuffer
}

func (t *transitionProcessor) processB(frame []int16, progress float64) []float64 {
	frameToFloat(frame, t.bBuffer)
	t.applyEQ(t.bBuffer, progress, false)
	t.applyFilter(t.bBuffer, progress, false)
	return t.bBuffer
}

func (t *transitionProcessor) applyGains(aBuf, bBuf []float64, progress, volume float64) {
	aGain, bGain := t.gains(progress)
	aGain *= volume
	bGain *= volume

	if !t.gainInitialized {
		t.previousAGain = aGain
		t.previousBGain = bGain
		t.gainInitialized = true
	}

	applyGainRamp(aBuf, t.previousAGain, aGain)
	applyGainRamp(bBuf, t.previousBGain, bGain)

	t.previousAGain = aGain
	t.previousBGain = bGain
}

func (t *transitionProcessor) hasTail() bool {
	switch t.recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		return t.reverbUnit != nil
	case EffectEchoHalfCutEnd:
		return t.echoUnit != nil && t.echoArmed
	}
	return false
}

type transitionTail struct {
	processor *transitionProcessor
	remaining int
	gain      float64
	buffer    []float64
}

func (t *transitionProcessor) makeTail(gain float64) *transitionTail {
	if !t.hasTail() {
		return nil
	}
	frames := reverbTailFrames
	if t.recipe.Effect == EffectEchoHalfCutEnd {
		frames = echoTailFrames
	}
	if t.tailBuffer == nil {
		t.tailBuffer = make([]float64, frameSize*channels)
	}
	return &transitionTail{
		processor: t,
		remaining: frames,
		gain:      gain,
		buffer:    t.tailBuffer,
	}
}

func (tail *transitionTail) apply(frame []int16) bool {
	if tail == nil || tail.remaining <= 0 {
		return false
	}

	silenceFloat(tail.buffer)
	switch tail.processor.recipe.Effect {
	case EffectReverbOutCenter, EffectReverbCutEnd, EffectReverbOutEnd:
		_, wet := tail.processor.effectMix(1)
		if wet <= 0 {
			wet = 0.6
		}
		tail.processor.reverbUnit.processStereo(tail.buffer, 0, wet)
	case EffectEchoHalfCutEnd:
		tail.processor.echoUnit.dry = 0
		tail.processor.echoUnit.wet = echoWet
		tail.processor.echoUnit.processStereo(tail.buffer)
	default:
		tail.remaining = 0
		return false
	}

	decay := float64(tail.remaining) / float64(reverbTailFrames)
	if tail.processor.recipe.Effect == EffectEchoHalfCutEnd {
		decay = float64(tail.remaining) / float64(echoTailFrames)
	}
	scale := tail.gain * clampUnit(decay)

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
