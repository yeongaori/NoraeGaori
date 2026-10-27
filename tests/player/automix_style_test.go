package player_test

import (
	"fmt"
	"testing"

	"noraegaori/internal/audio/transition"
)

var autoRecipe = transition.PresetRecipe(3)

type expectedStyle struct {
	category transition.Category
	style    string
	source   string
}

func checkResolved(t *testing.T, resolved *transition.Resolved, wants []expectedStyle) {
	t.Helper()
	for i := range wants {
		want := &wants[i]
		if got := transition.StyleOf(&resolved.Recipe, want.category); got != want.style {
			t.Errorf("%s = %q, want %q", want.category, got, want.style)
		}
		if got := resolved.Sources[want.category]; got != want.source {
			t.Errorf("%s source = %q, want %q", want.category, got, want.source)
		}
	}
}

func TestPresetsResolveToValidKeys(t *testing.T) {
	for _, preset := range []int{1, 2, 3, 4, 5, 8, 9, 10, 11, 17, 18, 19} {
		for _, category := range transition.StyleCategories() {
			if style := transition.StyleOf(transition.PresetRecipe(preset), category); !transition.ValidStyle(category, style) {
				t.Errorf("preset %d style %q for %q is not a valid style key", preset, style, category)
			}
		}
	}
}

func TestStylePrecedence(t *testing.T) {
	for _, category := range transition.StyleCategories() {
		t.Run(string(category), func(t *testing.T) {
			values := transition.StyleValues(category)
			guildStyle := values[len(values)-1]
			songStyle := values[1]
			autoStyle := transition.StyleOf(autoRecipe, category)

			cases := []struct {
				name       string
				guild      map[string]string
				song       map[string]string
				wantStyle  string
				wantSource string
			}{
				{"no overrides", nil, nil, autoStyle, "auto"},
				{"guild only", category.Override(guildStyle), nil, guildStyle, "guild"},
				{"song wins over guild", category.Override(guildStyle), category.Override(songStyle), songStyle, "song"},
				{"song auto defers to guild", category.Override(guildStyle), category.Override(transition.StyleAuto), guildStyle, "guild"},
				{"unknown values fall back to auto", category.Override("not_a_real_style"), category.Override(""), autoStyle, "auto"},
			}

			for i := range cases {
				testCase := &cases[i]
				resolved := transition.ResolveStyles(autoRecipe, testCase.guild, testCase.song)
				if got := transition.StyleOf(&resolved.Recipe, category); got != testCase.wantStyle {
					t.Errorf("%s: style = %q, want %q", testCase.name, got, testCase.wantStyle)
				}
				if got := resolved.Sources[category]; got != testCase.wantSource {
					t.Errorf("%s: source = %q, want %q", testCase.name, got, testCase.wantSource)
				}
			}
		})
	}
}

func TestResolveStylesOwnCategory(t *testing.T) {
	resolved := transition.ResolveStyles(autoRecipe, nil, transition.CategoryFXOut.Override("echo_half_cut_end"))

	for _, category := range transition.StyleCategories() {
		want := expectedStyle{category, transition.StyleOf(autoRecipe, category), "auto"}
		if category == transition.CategoryFXOut {
			want = expectedStyle{category, "echo_half_cut_end", "song"}
		}
		checkResolved(t, resolved, []expectedStyle{want})
	}
}

func TestPresetRecipeFadeBassSwap(t *testing.T) {
	resolved := transition.ResolveStyles(transition.PresetRecipe(transition.FadePreset), nil, nil)

	checkResolved(t, resolved, []expectedStyle{
		{transition.CategoryVolumeOut, "cross_shape", "auto"},
		{transition.CategoryVolumeIn, "cross_shape", "auto"},
		{transition.CategoryEQOut, "bass_fast", "auto"},
		{transition.CategoryEQIn, "bass_fast", "auto"},
		{transition.CategoryFilterOut, "none", "auto"},
		{transition.CategoryFXOut, "none", "auto"},
		{transition.CategoryLoop, "none", "auto"},
	})
}

func TestResolveStylesOverCrossfade(t *testing.T) {
	resolved := transition.ResolveStyles(transition.PresetRecipe(transition.NoPreset),
		transition.ExpandLegacy(transition.ShortcutEQ, "quick_bass"), transition.CategoryFXOut.Override("reverb_out_end"))

	checkResolved(t, resolved, []expectedStyle{
		{transition.CategoryEQOut, "bass_fast_one_bar_from_end", "guild"},
		{transition.CategoryFXOut, "reverb_out_end", "song"},
		{transition.CategoryVolumeOut, "cross_shape", "auto"},
	})
}

func TestOutroRecipeAutoNatural(t *testing.T) {
	outro := transition.OutroRecipe()
	for _, category := range []transition.Category{transition.CategoryEQOut, transition.CategoryFilterOut, transition.CategoryFXOut, transition.CategoryLoop} {
		if style := transition.StyleOf(outro, category); style != "none" {
			t.Errorf("%s = %q, want none so the last song is left alone", category, style)
		}
	}
	if !outro.IsOutroDefault() {
		t.Error("the auto outro is not recognized as the default")
	}
}

func TestOutroRecipeLayering(t *testing.T) {
	resolved := transition.ResolveStyles(transition.OutroRecipe(),
		transition.ExpandLegacy(transition.ShortcutVolume, "fadein_cutout"), transition.CategoryFXOut.Override("reverb_out_center"))

	checkResolved(t, resolved, []expectedStyle{
		{transition.CategoryVolumeOut, "fast_at_end", "guild"},
		{transition.CategoryFXOut, "reverb_out_center", "song"},
		{transition.CategoryFilterOut, "none", "auto"},
	})
}

func TestOutroRecipeVolumeShapes(t *testing.T) {
	distinct := map[string]bool{}

	for _, style := range transition.StyleValues(transition.CategoryVolumeOut)[1:] {
		recipe := transition.DefaultRecipe()
		recipe.Apply(transition.CategoryVolumeOut.Override(style))
		processor := transition.NewProcessor(&recipe, &transition.Window{Frames: 500, PeriodSec: 60.0 / 128, Bars: 8})
		samples := make([]string, 0, 9)
		for _, progress := range []float64{0, 0.25, 0.5, 0.505, 0.6, 0.8, 0.95, 0.999, 1} {
			gain, _ := processor.Gains(progress)
			samples = append(samples, fmt.Sprintf("%.3f", gain))
		}
		distinct[fmt.Sprint(samples)] = true
	}

	if want := len(transition.StyleValues(transition.CategoryVolumeOut)) - 1; len(distinct) != want {
		t.Errorf("got %d distinct outgoing curves, want %d: every outgoing volume style has its own shape", len(distinct), want)
	}
}
