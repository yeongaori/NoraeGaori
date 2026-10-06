package transition

type legacyStyle struct {
	name  string
	sides map[string]string
}

var shortcutTargets = map[Category][]Category{
	ShortcutVolume: {CategoryVolumeOut, CategoryVolumeIn},
	ShortcutEQ:     {CategoryEQOut, CategoryEQIn},
	ShortcutFilter: {CategoryFilterOut, CategoryFilterIn},
	ShortcutEffect: {CategoryFXOut},
}

var legacyStyles = map[Category][]legacyStyle{
	ShortcutVolume: {
		{"smooth", sides(ShortcutVolume, "cross_shape", "cross_shape")},
		{"overlap", sides(ShortcutVolume, "fast_at_end", "fast_at_start")},
		{"fadein_fadeout", sides(ShortcutVolume, "slow", "slow")},
		{"cutin_fadeout", sides(ShortcutVolume, "slow", "fast_at_start")},
		{"fadein_cutout", sides(ShortcutVolume, "fast_at_end", "slow")},
		{"fadein_fastout", sides(ShortcutVolume, "semi_fast_at_end", "slow")},
		{"crossfade", sides(ShortcutVolume, "crossfade", "crossfade")},
		{"cut", sides(ShortcutVolume, "fast", "fast")},
	},
	ShortcutEQ: {
		{"none", sides(ShortcutEQ, "none", "none")},
		{"center_bass_swap", sides(ShortcutEQ, "bass_fast", "bass_fast")},
		{"end_bass_swap", sides(ShortcutEQ, "bass_fast_at_end", "bass_fast_at_end")},
		{"start_bass_swap", sides(ShortcutEQ, "bass_fast_at_start", "bass_fast_at_start")},
		{"three_band_fade", sides(ShortcutEQ, "three_band", "three_band")},
		{"bass_mid_swap", sides(ShortcutEQ, "bass_and_mid_fast", "bass_and_mid_fast")},
		{"quick_bass", sides(ShortcutEQ, "bass_fast_one_bar_from_end", "bass_fast_at_end")},
		{"long_bass_cut", sides(ShortcutEQ, "bass_fast_at_start", "bass_fast_at_end")},
	},
	ShortcutFilter: {
		{"none", sides(ShortcutFilter, "none", "none")},
		{"lowpass_out", sides(ShortcutFilter, "low_pass", "none")},
		{"lowpass_in", sides(ShortcutFilter, "none", "low_pass")},
		{"lowpass_in_out", sides(ShortcutFilter, "low_pass", "low_pass")},
		{"lowpass_in_highpass_out", sides(ShortcutFilter, "high_pass", "low_pass")},
		{"highpass_out", sides(ShortcutFilter, "high_pass", "none")},
		{"highpass_in", sides(ShortcutFilter, "none", "high_pass")},
		{"highpass_in_out", sides(ShortcutFilter, "high_pass", "high_pass")},
		{"highpass_in_lowpass_out", sides(ShortcutFilter, "low_pass", "high_pass")},
		{"noise_riser", map[string]string{
			string(CategoryFilterOut): "none", string(CategoryFilterIn): "none", string(CategoryFXOut): "noise",
		}},
	},
	ShortcutEffect: legacyEffects(),
}

func sides(shortcut Category, out, in string) map[string]string {
	targets := shortcutTargets[shortcut]
	return map[string]string{string(targets[0]): out, string(targets[1]): in}
}

func legacyEffects() []legacyStyle {
	var effects []legacyStyle
	for _, entry := range fxOutNames {
		if entry.value < FXNoise {
			effects = append(effects, legacyStyle{entry.name, map[string]string{string(CategoryFXOut): entry.name}})
		}
	}
	for _, name := range []string{"spinback_one_beat", "spinback_two_beats", "spinback_four_beats"} {
		effects = append(effects, legacyStyle{name, map[string]string{string(CategoryLoop): name}})
	}
	return append(effects, legacyStyle{"vinyl_stop", map[string]string{string(CategoryLoop): "vinyl_stop_end"}})
}

var shortcutCategories = []Category{ShortcutVolume, ShortcutEQ, ShortcutFilter, ShortcutEffect}

func ShortcutCategories() []Category {
	return shortcutCategories[:len(shortcutCategories):len(shortcutCategories)]
}

var shortcutResets = buildShortcutResets()

func buildShortcutResets() map[Category]map[string]string {
	resets := make(map[Category]map[string]string, len(shortcutTargets))
	for category, targets := range shortcutTargets {
		reset := make(map[string]string, len(targets))
		for _, target := range targets {
			reset[string(target)] = StyleAuto
		}
		resets[category] = reset
	}
	return resets
}

func ExpandLegacy(category Category, value string) map[string]string {
	styles, isShortcut := legacyStyles[category]
	if !isShortcut {
		if ValidStyle(category, value) {
			return category.Override(value)
		}
		return nil
	}
	if value == StyleAuto {
		return shortcutResets[category]
	}
	for index := range styles {
		if styles[index].name == value {
			return styles[index].sides
		}
	}
	return nil
}
