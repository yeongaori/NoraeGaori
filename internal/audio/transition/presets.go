package transition

import (
	"crypto/sha1"
	"encoding/binary"
)

const (
	NoPreset        = 0
	FadePreset      = 1
	longPresetBars  = 4
	presetHashBytes = 8
)

var presetStyles = map[int][3]string{
	1:  {"smooth", "center_bass_swap", "none"},
	2:  {"overlap", "three_band_fade", "none"},
	3:  {"overlap", "end_bass_swap", "lowpass_in_highpass_out"},
	4:  {"fadein_fadeout", "center_bass_swap", "highpass_in_out"},
	5:  {"fadein_cutout", "center_bass_swap", "highpass_out"},
	8:  {"fadein_cutout", "center_bass_swap", "highpass_out"},
	9:  {"cutin_fadeout", "center_bass_swap", "highpass_in"},
	10: {"overlap", "center_bass_swap", "highpass_in_out"},
	11: {"cut", "none", "none"},
	17: {"overlap", "center_bass_swap", "lowpass_in_out"},
	18: {"fadein_cutout", "center_bass_swap", "lowpass_out"},
	19: {"overlap", "center_bass_swap", "lowpass_in_out"},
}

var (
	shortPresets = []int{1, 10, 19}
	longPresets  = []int{1, 2, 3, 4, 5, 17, 18, 8, 9, 10, 19}
)

var (
	presetCategories = [...]Category{ShortcutVolume, ShortcutEQ, ShortcutFilter}
	presetRecipes    = buildPresetRecipes()
	defaultRecipe    = DefaultRecipe()
)

func buildPresetRecipes() map[int]*Recipe {
	recipes := make(map[int]*Recipe, len(presetStyles))
	for preset, styles := range presetStyles {
		recipe := DefaultRecipe()
		for index, category := range presetCategories {
			recipe.Apply(ExpandLegacy(category, styles[index]))
		}
		recipes[preset] = &recipe
	}
	return recipes
}

func PresetRecipe(preset int) *Recipe {
	if recipe, ok := presetRecipes[preset]; ok {
		return recipe
	}
	return &defaultRecipe
}

func pickPreset(urlA, urlB string, bars, barA, barB int) int {
	message := append([]byte(urlA), urlB...)
	for _, value := range []int{bars, barA, barB} {
		message = binary.LittleEndian.AppendUint32(message, uint32(int32(value)))
	}
	sum := sha1.Sum(message)
	choice := binary.LittleEndian.Uint64(sum[:presetHashBytes])

	table := longPresets
	if bars < longPresetBars {
		table = shortPresets
	}
	return table[choice%uint64(len(table))]
}
