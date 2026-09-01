package transition

import (
	"fmt"
	"math"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
)

type Recipe struct {
	Volume VolumeStyle
	EQ     EQStyle
	Filter FilterStyle
	Effect EffectStyle
	Loop   LoopStyle
}

type StyleOverrides struct {
	Volume string
	EQ     string
	Filter string
	Effect string
	Loop   string
}

func DefaultRecipe() Recipe {
	return Recipe{
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
	return StyleAuto
}

func (r Recipe) String() string {
	return fmt.Sprintf("volume=%s eq=%s filter=%s effect=%s loop=%s",
		r.Volume, r.EQ, r.Filter, r.Effect, r.Loop)
}

var styleOrder = map[string][]string{
	"volume": {"smooth", "overlap", "fadein_fadeout", "cutin_fadeout", "fadein_cutout"},
	"eq":     {"none", "center_bass_swap", "end_bass_swap", "start_bass_swap", "three_band_fade", "quick_bass"},
	"filter": {"none", "lowpass_out", "lowpass_in", "lowpass_in_out", "lowpass_in_highpass_out"},
	"effect": {"none", "reverb_out_center", "reverb_cut_end", "reverb_out_end", "echo_half_cut_end"},
	"loop":   {"none", "one_beat", "two_beats", "four_beats", "eight_beats"},
}

func StyleValues(category string) []string {
	values, ok := styleOrder[category]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values)+1)
	out = append(out, StyleAuto)
	out = append(out, values...)
	return out
}

func ValidStyle(category, value string) bool {
	if value == StyleAuto {
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

func RecipeStyleMap(recipe Recipe) map[string]string {
	return map[string]string{
		"volume": recipe.Volume.String(),
		"eq":     recipe.EQ.String(),
		"filter": recipe.Filter.String(),
		"effect": recipe.Effect.String(),
		"loop":   recipe.Loop.String(),
	}
}

func overrideValue(overrides StyleOverrides, category string) string {
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

func AutoStyles(a, b *analysis.TrackAnalysis) map[string]string {
	return RecipeStyleMap(SelectRecipe(a, b))
}

func CrossfadeFrames(autoMix bool, autoMixBeats int, crossfadeSec float64, a *analysis.TrackAnalysis) (int, float64) {
	effectiveSec := FallbackCrossfadeSec
	if crossfadeSec > 0 {
		effectiveSec = crossfadeSec
	}
	if autoMix && a != nil {
		effectiveSec = float64(autoMixBeats) * a.PeriodSec
		if effectiveSec < CrossfadeMinSec {
			effectiveSec = CrossfadeMinSec
		}
		if effectiveSec > CrossfadeMaxSec {
			effectiveSec = CrossfadeMaxSec
		}
	}
	return int(effectiveSec * dsp.FramesPerSecond), effectiveSec
}

func ClampLoopStyle(loop LoopStyle, periodSec float64, crossfadeFrames int) (LoopStyle, int) {
	beats := LoopBeatCount(loop)
	if beats <= 0 || periodSec <= 0 {
		return LoopNone, 0
	}
	frames := int(math.Round(float64(beats) * periodSec * dsp.FramesPerSecond))
	if frames < 1 || frames*2 > crossfadeFrames {
		return LoopNone, 0
	}
	return loop, frames
}

func layerTransitionStyles(base Recipe, guild, song StyleOverrides) (Recipe, map[string]string, map[string]string) {
	recipe := base
	source := make(map[string]string, len(styleOrder))
	for category := range styleOrder {
		source[category] = "auto"
	}

	for _, layer := range []struct {
		name      string
		overrides StyleOverrides
	}{{"guild", guild}, {"song", song}} {
		for category := range styleOrder {
			value := overrideValue(layer.overrides, category)
			if value == "" || value == StyleAuto || !ValidStyle(category, value) {
				continue
			}
			source[category] = layer.name
		}
		recipe = ApplyStyleOverrides(recipe, layer.overrides)
	}

	return recipe, RecipeStyleMap(recipe), source
}

func ResolveStyles(a, b *analysis.TrackAnalysis, autoSelect bool, guild, song StyleOverrides) (Recipe, map[string]string, map[string]string) {
	base := DefaultRecipe()
	if autoSelect {
		base = SelectRecipe(a, b)
	}
	return layerTransitionStyles(base, guild, song)
}

func ResolveOutroStyles(a *analysis.TrackAnalysis, autoSelect bool, guild, song StyleOverrides) (Recipe, map[string]string, map[string]string) {
	base := DefaultRecipe()
	if autoSelect {
		base = selectOutroRecipe(a)
	}
	return layerTransitionStyles(base, guild, song)
}

func AutoOutroStyles(a *analysis.TrackAnalysis) map[string]string {
	return RecipeStyleMap(selectOutroRecipe(a))
}

func ApplyStyleOverrides(recipe Recipe, overrides StyleOverrides) Recipe {
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

func LoopBeatCount(style LoopStyle) int {
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

func selectOutroRecipe(a *analysis.TrackAnalysis) Recipe {
	recipe := DefaultRecipe()
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

func SelectRecipe(a, b *analysis.TrackAnalysis) Recipe {
	recipe := DefaultRecipe()
	if a == nil || b == nil || a.BPM <= 0 || b.BPM <= 0 {
		return recipe
	}

	bpmDelta := analysis.TempoDelta(a.BPM, b.BPM)
	gridSolid := a.PeriodSec > 0 && b.PeriodSec > 0
	distance := analysis.CamelotDistance(a, b)
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
