package transition

const (
	CrossfadeMinSec      = 3.0
	CrossfadeMaxSec      = 20.0
	FallbackCrossfadeSec = 8.0
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
	StyleAuto           = "auto"
	EQKillDB            = -40.0
	EQLowFreq           = 250.0
	EQMidFreq           = 1000.0
	EQHighFreq          = 4000.0
	EQShelfQ            = 0.707
	EQMidQ              = 0.9
	filterQ             = 0.9
	filterOpenFreq      = 20000.0
	filterClosedFreq    = 200.0
	highPassRestFreq    = 25.0
	highPassPeakFreq    = 3000.0
	filterOpenThreshold = 18000.0
	filterRestThreshold = 30.0
	overlapHeadroom     = 0.85
	ReverbTailFrames    = 140
	EchoTailFrames      = 170
	echoFeedback        = 0.55
	echoWet             = 0.85
	reverbMaxWet        = 0.9
	minBeatFraction     = 0.02
	maxBeatFraction     = 0.5
	defaultBeatFraction = 0.1
	bpmMatchTolerance   = 0.03
	bpmLooseTolerance   = 0.08
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
