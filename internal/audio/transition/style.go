package transition

const (
	CrossfadeMinSec      = 3.0
	CrossfadeMaxSec      = 20.0
	FallbackCrossfadeSec = 8.0
)

type VolumeStyle int

const (
	VolumeCrossShape VolumeStyle = iota
	VolumeCrossfade
	VolumeSlow
	VolumeFast
	VolumeFastAtEdge
	VolumeSemiFastAtEnd
	VolumeSwitcharoo
)

type EQStyle int

const (
	EQNone EQStyle = iota
	EQThreeBand
	EQBassFade
	EQBassCrossfade
	EQBassFast
	EQBassAndMidFast
	EQMidFast
	EQHiFast
	EQBassFastAtEnd
	EQBassFastAtStart
	EQBassFastOneBarFromEnd
)

type FilterStyle int

const (
	FilterNone FilterStyle = iota
	FilterLowPass
	FilterHighPass
)

type FXStyle int

const (
	FXNone FXStyle = iota
	FXReverbOutCenter
	FXReverbCutEnd
	FXReverbOutEnd
	FXEchoHalfCutEnd
	FXEchoHalfOutEnd
	FXEchoThreeQuarterCutEnd
	FXEchoThreeQuarterOutEnd
	FXEchoBeatCutEnd
	FXEchoBeatOutEnd
	FXDelayHalfCutEnd
	FXDelayThreeQuarterCutEnd
	FXNoise
	FXNoiseAtEnd
	FXDelayRamp
	FXDelayRampAtEnd
	FXDelayOneBar
	FXDelayOneBarAtEnd
	FXPhaser
	FXBitcrusher
	FXRoll
	FXSlipRoll
)

type LoopStyle int

const (
	LoopNone LoopStyle = iota
	LoopOneBeat
	LoopTwoBeats
	LoopFourBeats
	LoopEightBeats
	LoopSixteenBeats
	LoopRoll
	LoopRollAtEnd
	LoopSlipRoll
	LoopSlipRollAtEnd
	LoopSpinbackOneBeat
	LoopSpinbackTwoBeats
	LoopSpinbackFourBeats
	LoopVinylStopCenter
	LoopVinylStopCenterShort
	LoopVinylStopEnd
	LoopVinylStopEndShort
)

type BeatmatchMode int

const (
	BeatmatchAuto BeatmatchMode = iota
	BeatmatchOn
	BeatmatchOff
)

const (
	StyleAuto         = "auto"
	paramBlockSamples = 64
	minimumBars       = 2
	quarterBeatParts  = 16
	tickBeatParts     = 256
	eqUnity           = 0.5
	eqCut             = 0.2
	eqKill            = 0.0
	wetPeak           = 0.5
	wetCutEnd         = 0.99999
	noiseColourStart  = 0.62
	noiseColourEnd    = 0.83
)

type named[T comparable] struct {
	name  string
	value T
}

type catalogue[T comparable] []named[T]

func (c catalogue[T]) lookup(name string) (T, bool) {
	for _, entry := range c {
		if entry.name == name {
			return entry.value, true
		}
	}
	var zero T
	return zero, false
}

func (c catalogue[T]) nameOf(value T) string {
	for _, entry := range c {
		if entry.value == value {
			return entry.name
		}
	}
	return StyleAuto
}

func (c catalogue[T]) names() []string {
	names := make([]string, 0, len(c))
	for _, entry := range c {
		names = append(names, entry.name)
	}
	return names
}

var volumeOutNames = catalogue[VolumeStyle]{
	{"crossfade", VolumeCrossfade},
	{"cross_shape", VolumeCrossShape},
	{"slow", VolumeSlow},
	{"fast", VolumeFast},
	{"fast_at_end", VolumeFastAtEdge},
	{"semi_fast_at_end", VolumeSemiFastAtEnd},
	{"switcharoo", VolumeSwitcharoo},
}

var volumeInNames = catalogue[VolumeStyle]{
	{"crossfade", VolumeCrossfade},
	{"cross_shape", VolumeCrossShape},
	{"slow", VolumeSlow},
	{"fast", VolumeFast},
	{"fast_at_start", VolumeFastAtEdge},
	{"switcharoo", VolumeSwitcharoo},
}

var eqInNames = catalogue[EQStyle]{
	{"none", EQNone},
	{"three_band", EQThreeBand},
	{"bass_crossfade", EQBassCrossfade},
	{"bass_fast", EQBassFast},
	{"bass_and_mid_fast", EQBassAndMidFast},
	{"mid_fast", EQMidFast},
	{"hi_fast", EQHiFast},
	{"bass_fast_at_end", EQBassFastAtEnd},
	{"bass_fast_at_start", EQBassFastAtStart},
}

var eqOutNames = catalogue[EQStyle]{
	{"none", EQNone},
	{"three_band", EQThreeBand},
	{"bass_fade", EQBassFade},
	{"bass_crossfade", EQBassCrossfade},
	{"bass_fast", EQBassFast},
	{"bass_and_mid_fast", EQBassAndMidFast},
	{"mid_fast", EQMidFast},
	{"hi_fast", EQHiFast},
	{"bass_fast_at_end", EQBassFastAtEnd},
	{"bass_fast_at_start", EQBassFastAtStart},
	{"bass_fast_one_bar_from_end", EQBassFastOneBarFromEnd},
}

var filterNames = catalogue[FilterStyle]{
	{"none", FilterNone},
	{"low_pass", FilterLowPass},
	{"high_pass", FilterHighPass},
}

var fxOutNames = catalogue[FXStyle]{
	{"none", FXNone},
	{"reverb_out_center", FXReverbOutCenter},
	{"reverb_cut_end", FXReverbCutEnd},
	{"reverb_out_end", FXReverbOutEnd},
	{"echo_half_cut_end", FXEchoHalfCutEnd},
	{"echo_half_out_end", FXEchoHalfOutEnd},
	{"echo_three_quarter_cut_end", FXEchoThreeQuarterCutEnd},
	{"echo_three_quarter_out_end", FXEchoThreeQuarterOutEnd},
	{"echo_beat_cut_end", FXEchoBeatCutEnd},
	{"echo_beat_out_end", FXEchoBeatOutEnd},
	{"delay_half_cut_end", FXDelayHalfCutEnd},
	{"delay_three_quarter_cut_end", FXDelayThreeQuarterCutEnd},
	{"noise", FXNoise},
	{"noise_at_end", FXNoiseAtEnd},
	{"delay_ramp", FXDelayRamp},
	{"delay_ramp_at_end", FXDelayRampAtEnd},
	{"delay_one_bar", FXDelayOneBar},
	{"delay_one_bar_at_end", FXDelayOneBarAtEnd},
	{"phaser", FXPhaser},
	{"bitcrusher", FXBitcrusher},
}

var fxInNames = catalogue[FXStyle]{
	{"none", FXNone},
	{"roll", FXRoll},
	{"slip_roll", FXSlipRoll},
	{"delay_one_bar", FXDelayOneBar},
	{"delay_one_bar_at_end", FXDelayOneBarAtEnd},
	{"phaser", FXPhaser},
	{"bitcrusher", FXBitcrusher},
}

var loopNames = catalogue[LoopStyle]{
	{"none", LoopNone},
	{"one_beat", LoopOneBeat},
	{"two_beats", LoopTwoBeats},
	{"four_beats", LoopFourBeats},
	{"eight_beats", LoopEightBeats},
	{"sixteen_beats", LoopSixteenBeats},
	{"roll", LoopRoll},
	{"roll_at_end", LoopRollAtEnd},
	{"slip_roll", LoopSlipRoll},
	{"slip_roll_at_end", LoopSlipRollAtEnd},
	{"spinback_one_beat", LoopSpinbackOneBeat},
	{"spinback_two_beats", LoopSpinbackTwoBeats},
	{"spinback_four_beats", LoopSpinbackFourBeats},
	{"vinyl_stop_center", LoopVinylStopCenter},
	{"vinyl_stop_center_short", LoopVinylStopCenterShort},
	{"vinyl_stop_end", LoopVinylStopEnd},
	{"vinyl_stop_end_short", LoopVinylStopEndShort},
}

var presetNames = catalogue[int]{
	{"1", 1}, {"2", 2}, {"3", 3}, {"4", 4}, {"5", 5}, {"9", 9}, {"10", 10}, {"11", 11}, {"17", 17}, {"18", 18},
}

var lengthNames = catalogue[int]{
	{"two_bars", 2},
	{"four_bars", 4},
	{"eight_bars", 8},
	{"sixteen_bars", 16},
}

var beatmatchNames = catalogue[BeatmatchMode]{
	{"on", BeatmatchOn},
	{"off", BeatmatchOff},
}
