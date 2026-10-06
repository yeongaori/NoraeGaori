package transition_test

import (
	"slices"
	"testing"

	"noraegaori/internal/audio/transition"
)

func allCategories() []transition.Category {
	return slices.Concat(transition.StyleCategories(), transition.SettingCategories(), transition.ShortcutCategories())
}

func overridesOf(choices ...styleChoice) map[string]string {
	overrides := make(map[string]string, len(choices))
	for _, choice := range choices {
		overrides[string(choice.category)] = choice.style
	}
	return overrides
}

func TestStyleCatalogueValidates(t *testing.T) {
	for _, category := range allCategories() {
		t.Run(string(category), func(t *testing.T) {
			values := transition.StyleValues(category)
			if len(values) < 2 || values[0] != transition.StyleAuto {
				t.Fatalf("category advertises %v, want auto followed by styles", values)
			}
			if len(values) > 25 {
				t.Errorf("category advertises %d values, want at most 25 to fit a Discord select menu", len(values))
			}
			for _, value := range values {
				if !transition.ValidStyle(category, value) {
					t.Errorf("advertised value %q is rejected by the validator", value)
				}
			}
		})
	}
}

func TestEveryKnownCategoryParses(t *testing.T) {
	for _, category := range allCategories() {
		if parsed, ok := transition.ParseCategory(string(category)); !ok || parsed != category {
			t.Errorf("%s parses as %q (%t), want itself", category, parsed, ok)
		}
	}
	for _, name := range []string{"", "fx", "volume_middle", "Loop"} {
		if _, ok := transition.ParseCategory(name); ok {
			t.Errorf("%q parses as a category, want it rejected", name)
		}
	}
}

func TestEveryAdvertisedStyleRoundTripsThroughItsName(t *testing.T) {
	for _, category := range transition.StyleCategories() {
		for _, value := range legacyNames(category) {
			recipe := combinedRecipe(sideStyle(category, value))
			if got := transition.StyleOf(recipe, category); got != value {
				t.Errorf("%s %q came back as %q", category, value, got)
			}
		}
	}
}

func TestSideOnlyStylesStayOnTheirSide(t *testing.T) {
	cases := map[transition.Category][]string{
		transition.CategoryVolumeIn:  {"fast_at_end", "semi_fast_at_end"},
		transition.CategoryVolumeOut: {"fast_at_start"},
		transition.CategoryEQIn:      {"bass_fade", "bass_fast_one_bar_from_end"},
		transition.CategoryFXOut:     {"roll", "slip_roll"},
		transition.CategoryFXIn:      {"noise", "delay_ramp", "reverb_cut_end"},
	}
	for category, values := range cases {
		for _, value := range values {
			if transition.ValidStyle(category, value) {
				t.Errorf("%s accepts %q, which belongs to the other side", category, value)
			}
		}
	}
}

func TestUnknownCategoryRejected(t *testing.T) {
	if values := transition.StyleValues("bogus"); values != nil {
		t.Errorf("got %v, want nil", values)
	}
	if transition.ValidStyle("bogus", "auto") {
		t.Error("an unknown category accepted auto")
	}
}

func TestUnknownStyleRejected(t *testing.T) {
	if transition.ValidStyle(transition.CategoryEQOut, "super_bass") {
		t.Error("eq_out accepted the unknown style super_bass")
	}
}

func TestUnknownOverridesAreIgnored(t *testing.T) {
	resolved := transition.ResolveStyles(transition.PresetRecipe(2),
		map[string]string{"fx": "phaser", "volume": "cut", "eq_out": "super_bass"},
		map[string]string{"loop": "spin", "": "none"})
	if resolved.Recipe != *transition.PresetRecipe(2) {
		t.Errorf("unknown overrides changed the recipe to %s", &resolved.Recipe)
	}
	for category, source := range resolved.Sources {
		if source != "auto" {
			t.Errorf("%s comes from %s, want auto when every override is unknown", category, source)
		}
	}
}

func TestStoredStyleNamesStayValid(t *testing.T) {
	stored := map[transition.Category][]string{
		transition.ShortcutVolume: {"smooth", "overlap", "fadein_fadeout", "cutin_fadeout", "fadein_cutout", "fadein_fastout", "crossfade", "cut"},
		transition.ShortcutEQ:     {"none", "center_bass_swap", "end_bass_swap", "start_bass_swap", "three_band_fade", "bass_mid_swap", "quick_bass", "long_bass_cut"},
		transition.ShortcutFilter: {"none", "lowpass_out", "lowpass_in", "lowpass_in_out", "lowpass_in_highpass_out", "highpass_out", "highpass_in", "highpass_in_out", "highpass_in_lowpass_out", "noise_riser"},
		transition.ShortcutEffect: {"none", "reverb_out_center", "reverb_cut_end", "reverb_out_end", "echo_half_cut_end", "echo_half_out_end",
			"echo_three_quarter_cut_end", "echo_three_quarter_out_end", "echo_beat_cut_end", "echo_beat_out_end",
			"delay_half_cut_end", "delay_three_quarter_cut_end", "spinback_one_beat", "spinback_two_beats", "spinback_four_beats", "vinyl_stop"},
		transition.CategoryLoop: {"none", "one_beat", "two_beats", "four_beats", "eight_beats", "sixteen_beats", "roll"},
	}
	for category, values := range stored {
		for _, value := range values {
			expanded := transition.ExpandLegacy(category, value)
			if len(expanded) == 0 {
				t.Errorf("%s %q saved by an earlier version no longer expands", category, value)
			}
			for side, style := range expanded {
				parsed, ok := transition.ParseCategory(side)
				if !ok || !transition.ValidStyle(parsed, style) {
					t.Errorf("%s %q expands to the invalid %s %q", category, value, side, style)
				}
			}
		}
	}
}

func TestCombinedStylesExpandToTheirSidePairs(t *testing.T) {
	cases := []struct {
		category transition.Category
		value    string
		want     map[string]string
	}{
		{transition.ShortcutVolume, "smooth", overridesOf(sideStyle(transition.CategoryVolumeOut, "cross_shape"), sideStyle(transition.CategoryVolumeIn, "cross_shape"))},
		{transition.ShortcutVolume, "overlap", overridesOf(sideStyle(transition.CategoryVolumeOut, "fast_at_end"), sideStyle(transition.CategoryVolumeIn, "fast_at_start"))},
		{transition.ShortcutVolume, "cutin_fadeout", overridesOf(sideStyle(transition.CategoryVolumeOut, "slow"), sideStyle(transition.CategoryVolumeIn, "fast_at_start"))},
		{transition.ShortcutVolume, "fadein_fastout", overridesOf(sideStyle(transition.CategoryVolumeOut, "semi_fast_at_end"), sideStyle(transition.CategoryVolumeIn, "slow"))},
		{transition.ShortcutEQ, "quick_bass", overridesOf(sideStyle(transition.CategoryEQOut, "bass_fast_one_bar_from_end"), sideStyle(transition.CategoryEQIn, "bass_fast_at_end"))},
		{transition.ShortcutEQ, "long_bass_cut", overridesOf(sideStyle(transition.CategoryEQOut, "bass_fast_at_start"), sideStyle(transition.CategoryEQIn, "bass_fast_at_end"))},
		{transition.ShortcutFilter, "lowpass_in_highpass_out", overridesOf(sideStyle(transition.CategoryFilterOut, "high_pass"), sideStyle(transition.CategoryFilterIn, "low_pass"))},
		{transition.ShortcutFilter, "noise_riser", overridesOf(sideStyle(transition.CategoryFilterOut, "none"), sideStyle(transition.CategoryFilterIn, "none"), sideStyle(transition.CategoryFXOut, "noise"))},
		{transition.ShortcutEffect, "echo_half_out_end", overridesOf(sideStyle(transition.CategoryFXOut, "echo_half_out_end"))},
		{transition.ShortcutEffect, "spinback_two_beats", overridesOf(sideStyle(transition.CategoryLoop, "spinback_two_beats"))},
		{transition.ShortcutEffect, "vinyl_stop", overridesOf(sideStyle(transition.CategoryLoop, "vinyl_stop_end"))},
		{transition.ShortcutVolume, "auto", overridesOf(sideStyle(transition.CategoryVolumeOut, "auto"), sideStyle(transition.CategoryVolumeIn, "auto"))},
		{transition.ShortcutEffect, "auto", overridesOf(sideStyle(transition.CategoryFXOut, "auto"))},
		{transition.CategoryFXIn, "phaser", overridesOf(sideStyle(transition.CategoryFXIn, "phaser"))},
	}
	for _, c := range cases {
		got := transition.ExpandLegacy(c.category, c.value)
		if len(got) != len(c.want) {
			t.Errorf("%s %q expands to %v, want %v", c.category, c.value, got, c.want)
			continue
		}
		for key, value := range c.want {
			if got[key] != value {
				t.Errorf("%s %q expands to %v, want %v", c.category, c.value, got, c.want)
				break
			}
		}
	}
	if got := transition.ExpandLegacy(transition.ShortcutVolume, "garbage"); got != nil {
		t.Errorf("an unknown legacy value expands to %v, want nil", got)
	}
	if got := transition.ExpandLegacy(transition.CategoryFXIn, "noise"); got != nil {
		t.Errorf("an invalid side value expands to %v, want nil", got)
	}
}

func TestSharedCataloguesCannotBeGrownThroughAppend(t *testing.T) {
	values := transition.StyleValues(transition.CategoryEQOut)
	if cap(values) != len(values) {
		t.Errorf("eq_out values have spare capacity %d over %d, want an append to copy", cap(values), len(values))
	}
	_ = append(values, "intruder")
	if transition.ValidStyle(transition.CategoryEQOut, "intruder") {
		t.Error("an append by one caller changed the shared catalogue")
	}
	for _, list := range [][]transition.Category{transition.StyleCategories(), transition.SettingCategories(),
		transition.ShortcutCategories(), transition.ShortcutTargets(transition.ShortcutVolume)} {
		if cap(list) != len(list) {
			t.Errorf("%v has spare capacity, want an append to copy", list)
		}
	}
}

func TestCatalogueLookupsDoNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(100, func() {
		_ = transition.StyleValues(transition.CategoryFXOut)
		_ = transition.ExpandLegacy(transition.ShortcutVolume, "smooth")
		_ = transition.ExpandLegacy(transition.ShortcutEffect, "auto")
		_ = transition.PresetRecipe(3)
		_ = transition.OutroRecipe()
	})
	if allocations != 0 {
		t.Errorf("catalogue lookups allocate %.0f times, want them to share the built tables", allocations)
	}
}

func TestPresetRecipesMatchTheirCombinedStyles(t *testing.T) {
	want := combinedRecipe(volumeStyle("overlap"), eqStyle("end_bass_swap"), filterStyle("lowpass_in_highpass_out"))
	if got := transition.PresetRecipe(3); *got != *want {
		t.Errorf("preset 3 = %s, want %s", got, want)
	}
	if got := transition.PresetRecipe(11); *got != *combinedRecipe(volumeStyle("cut")) {
		t.Errorf("preset 11 = %s, want the simple cut", got)
	}
	if got := transition.PresetRecipe(transition.NoPreset); *got != *combinedRecipe() {
		t.Errorf("no preset = %s, want the default recipe", got)
	}
}

func TestOverridesApplyOnlyForKnownValues(t *testing.T) {
	recipe := combinedRecipe()
	recipe.Apply(overridesOf(
		sideStyle(transition.CategoryVolumeOut, "fast_at_end"),
		sideStyle(transition.CategoryEQOut, transition.StyleAuto),
		sideStyle(transition.CategoryFilterIn, "garbage"),
		sideStyle(transition.CategoryFXOut, "reverb_cut_end"),
		sideStyle(transition.CategoryLoop, ""),
		eqStyle("three_band_fade"),
	))

	if recipe.Out.Volume != transition.VolumeFastAtEdge {
		t.Errorf("volume out = %s, want the known override fast_at_end", transition.StyleOf(recipe, transition.CategoryVolumeOut))
	}
	if recipe.Out.EQ != transition.EQNone || recipe.In.EQ != transition.EQNone {
		t.Errorf("eq = %s, want none for auto, empty and shortcut keys", recipe)
	}
	if recipe.In.Filter != transition.FilterNone {
		t.Errorf("filter in = %s, want none for an unknown override", transition.StyleOf(recipe, transition.CategoryFilterIn))
	}
	if recipe.Out.FX != transition.FXReverbCutEnd {
		t.Errorf("fx out = %s, want the known override reverb_cut_end", transition.StyleOf(recipe, transition.CategoryFXOut))
	}
	if recipe.Loop != transition.LoopNone {
		t.Errorf("loop = %s, want none for an empty override", transition.StyleOf(recipe, transition.CategoryLoop))
	}
}

func TestSongOverridesBeatGuildOverrides(t *testing.T) {
	resolved := transition.ResolveStyles(transition.PresetRecipe(2),
		overridesOf(sideStyle(transition.CategoryVolumeOut, "fast"), sideStyle(transition.CategoryEQIn, "none")),
		transition.CategoryVolumeOut.Override("crossfade"))

	if resolved.Recipe.Out.Volume != transition.VolumeCrossfade || resolved.Sources[transition.CategoryVolumeOut] != "song" {
		t.Errorf("volume out = %s from %s, want crossfade from the song",
			transition.StyleOf(&resolved.Recipe, transition.CategoryVolumeOut), resolved.Sources[transition.CategoryVolumeOut])
	}
	if resolved.Recipe.In.EQ != transition.EQNone || resolved.Sources[transition.CategoryEQIn] != "guild" {
		t.Errorf("eq in = %s from %s, want none from the guild",
			transition.StyleOf(&resolved.Recipe, transition.CategoryEQIn), resolved.Sources[transition.CategoryEQIn])
	}
	if resolved.Recipe.Out.EQ != transition.EQThreeBand || resolved.Sources[transition.CategoryEQOut] != "auto" {
		t.Errorf("eq out = %s from %s, want the preset's three_band left alone",
			transition.StyleOf(&resolved.Recipe, transition.CategoryEQOut), resolved.Sources[transition.CategoryEQOut])
	}
	if preset := transition.PresetRecipe(2); preset.Out.Volume != transition.VolumeFastAtEdge {
		t.Errorf("resolving changed the shared preset to %s", preset)
	}
}

func TestSettingsResolveFromSongOverrides(t *testing.T) {
	settings := transition.ResolveSettings(overridesOf(
		sideStyle(transition.CategoryPreset, "3"), sideStyle(transition.CategoryLength, "eight_bars"),
		sideStyle(transition.CategoryBeatmatch, "off"), sideStyle(transition.CategoryFXOut, "phaser"),
	))
	if settings != (transition.Settings{Preset: 3, Bars: 8, Beatmatch: transition.BeatmatchOff}) {
		t.Errorf("settings = %+v, want preset 3, 8 bars, beatmatch off", settings)
	}
	invalid := overridesOf(sideStyle(transition.CategoryPreset, "7"), sideStyle(transition.CategoryLength, "3"), sideStyle(transition.CategoryBeatmatch, "maybe"))
	if settings := transition.ResolveSettings(invalid); settings != (transition.Settings{}) {
		t.Errorf("invalid settings resolved to %+v, want all auto", settings)
	}
}

func TestLoopBeatCounts(t *testing.T) {
	counts := []struct {
		style transition.LoopStyle
		want  int
	}{
		{transition.LoopOneBeat, 1},
		{transition.LoopTwoBeats, 2},
		{transition.LoopFourBeats, 4},
		{transition.LoopEightBeats, 8},
		{transition.LoopSixteenBeats, 16},
		{transition.LoopRoll, 1},
		{transition.LoopSlipRollAtEnd, 1},
		{transition.LoopSpinbackFourBeats, 0},
		{transition.LoopNone, 0},
	}

	for _, count := range counts {
		recipe := transition.Recipe{Loop: count.style}
		if got := transition.LoopBeatCount(count.style); got != count.want {
			t.Errorf("%s = %d beats, want %d", transition.StyleOf(&recipe, transition.CategoryLoop), got, count.want)
		}
	}
}

func clampedStyle(category transition.Category, style string, periodSec float64, frames int) string {
	resolved := transition.ResolveStyles(combinedRecipe(sideStyle(category, style)), nil, nil)
	resolved.ClampRolls(periodSec, frames)
	return transition.StyleOf(&resolved.Recipe, category)
}

func TestLoopsMustFitTheOverlap(t *testing.T) {
	cases := []struct {
		loop   string
		frames int
		want   string
	}{
		{"sixteen_beats", 400, "sixteen_beats"},
		{"sixteen_beats", 399, "none"},
		{"roll", 100, "roll"},
		{"roll", 99, "none"},
		{"roll_at_end", 200, "roll_at_end"},
		{"roll_at_end", 199, "none"},
		{"slip_roll", 200, "slip_roll"},
		{"slip_roll", 199, "none"},
		{"slip_roll_at_end", 400, "slip_roll_at_end"},
		{"slip_roll_at_end", 399, "none"},
		{"two_beats", 0, "none"},
		{"spinback_one_beat", 0, "spinback_one_beat"},
	}
	for _, c := range cases {
		if got := clampedStyle(transition.CategoryLoop, c.loop, 0.5, c.frames); got != c.want {
			t.Errorf("%s over %d frames = %s, want %s", c.loop, c.frames, got, c.want)
		}
	}
	if got := clampedStyle(transition.CategoryLoop, "two_beats", 0, 400); got != "none" {
		t.Errorf("loop without a beat grid = %s, want none", got)
	}
}

func TestIncomingRollsMustFitTheOverlap(t *testing.T) {
	cases := []struct {
		fx     string
		frames int
		want   string
	}{
		{"roll", 100, "roll"},
		{"roll", 99, "none"},
		{"slip_roll", 25, "slip_roll"},
		{"slip_roll", 24, "none"},
		{"phaser", 1, "phaser"},
	}
	for _, c := range cases {
		if got := clampedStyle(transition.CategoryFXIn, c.fx, 0.5, c.frames); got != c.want {
			t.Errorf("incoming %s over %d frames = %s, want %s", c.fx, c.frames, got, c.want)
		}
	}
}

func TestOutroRecipeLeavesTheSongAlone(t *testing.T) {
	recipe := transition.OutroRecipe()
	if recipe.Out.EQ != transition.EQNone || recipe.Out.Filter != transition.FilterNone || recipe.Out.FX != transition.FXNone || recipe.Loop != transition.LoopNone {
		t.Errorf("outro recipe %s, want no EQ, filter, effect or loop", recipe)
	}
	processor := transition.NewProcessor(recipe, &transition.Window{Frames: 200, PeriodSec: 0.5, Bars: 4})
	if out, _ := processor.Gains(0.99); out != 1 {
		t.Errorf("outro volume at 99%% = %.3f, want the song at full level until it ends", out)
	}
}

func TestOnlyOutgoingChoicesChangeTheOutro(t *testing.T) {
	for _, c := range []struct {
		overrides map[string]string
		isDefault bool
	}{
		{nil, true},
		{overridesOf(sideStyle(transition.CategoryVolumeIn, "crossfade"), sideStyle(transition.CategoryFXIn, "phaser"), sideStyle(transition.CategoryEQIn, "hi_fast")), true},
		{transition.CategoryEQOut.Override("hi_fast"), false},
		{transition.CategoryLoop.Override("vinyl_stop_end"), false},
		{transition.CategoryFXOut.Override("phaser"), false},
	} {
		resolved := transition.ResolveStyles(transition.OutroRecipe(), c.overrides, nil)
		if got := resolved.Recipe.IsOutroDefault(); got != c.isDefault {
			t.Errorf("overrides %v give an outro default of %t, want %t", c.overrides, got, c.isDefault)
		}
	}
}
