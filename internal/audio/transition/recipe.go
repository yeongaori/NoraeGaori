package transition

import (
	"slices"
	"strings"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/logger"
)

type Side struct {
	Volume VolumeStyle
	EQ     EQStyle
	Filter FilterStyle
	FX     FXStyle
}

type Recipe struct {
	Out  Side
	In   Side
	Loop LoopStyle
}

type Settings struct {
	Preset    int
	Bars      int
	Beatmatch BeatmatchMode
}

func DefaultRecipe() Recipe {
	return Recipe{Out: Side{Volume: VolumeCrossShape}, In: Side{Volume: VolumeCrossShape}}
}

var outroRecipe = Recipe{Out: Side{Volume: VolumeFastAtEdge}, In: Side{Volume: VolumeFastAtEdge}}

func OutroRecipe() *Recipe {
	return &outroRecipe
}

func (r *Recipe) IsOutroDefault() bool {
	return r.Out == outroRecipe.Out && r.Loop == outroRecipe.Loop
}

func ShortcutTargets(category Category) []Category {
	targets := shortcutTargets[category]
	return targets[:len(targets):len(targets)]
}

func (r *Recipe) String() string {
	parts := make([]string, 0, len(styleCategories))
	for _, category := range styleCategories {
		parts = append(parts, string(category)+"="+categories[category].readStyle(r))
	}
	return strings.Join(parts, " ")
}

type Category string

const (
	CategoryVolumeOut Category = "volume_out"
	CategoryVolumeIn  Category = "volume_in"
	CategoryEQOut     Category = "eq_out"
	CategoryEQIn      Category = "eq_in"
	CategoryFilterOut Category = "filter_out"
	CategoryFilterIn  Category = "filter_in"
	CategoryFXOut     Category = "fx_out"
	CategoryFXIn      Category = "fx_in"
	CategoryLoop      Category = "loop"
	CategoryPreset    Category = "preset"
	CategoryLength    Category = "length"
	CategoryBeatmatch Category = "beatmatch"
	ShortcutVolume    Category = "volume"
	ShortcutEQ        Category = "eq"
	ShortcutFilter    Category = "filter"
	ShortcutEffect    Category = "effect"
)

type categoryInfo struct {
	names        []string
	readStyle    func(*Recipe) string
	writeStyle   func(*Recipe, string)
	writeSetting func(*Settings, string)
}

var (
	styleCategories = []Category{
		CategoryVolumeOut, CategoryVolumeIn, CategoryEQOut, CategoryEQIn, CategoryFilterOut, CategoryFilterIn,
		CategoryFXOut, CategoryFXIn, CategoryLoop,
	}
	settingCategories = []Category{CategoryPreset, CategoryLength, CategoryBeatmatch}
)

var categories = map[Category]categoryInfo{
	CategoryVolumeOut: styleCategory(volumeOutNames, func(r *Recipe) *VolumeStyle { return &r.Out.Volume }),
	CategoryVolumeIn:  styleCategory(volumeInNames, func(r *Recipe) *VolumeStyle { return &r.In.Volume }),
	CategoryEQOut:     styleCategory(eqOutNames, func(r *Recipe) *EQStyle { return &r.Out.EQ }),
	CategoryEQIn:      styleCategory(eqInNames, func(r *Recipe) *EQStyle { return &r.In.EQ }),
	CategoryFilterOut: styleCategory(filterNames, func(r *Recipe) *FilterStyle { return &r.Out.Filter }),
	CategoryFilterIn:  styleCategory(filterNames, func(r *Recipe) *FilterStyle { return &r.In.Filter }),
	CategoryFXOut:     styleCategory(fxOutNames, func(r *Recipe) *FXStyle { return &r.Out.FX }),
	CategoryFXIn:      styleCategory(fxInNames, func(r *Recipe) *FXStyle { return &r.In.FX }),
	CategoryLoop:      styleCategory(loopNames, func(r *Recipe) *LoopStyle { return &r.Loop }),
	CategoryPreset:    settingCategory(presetNames, func(s *Settings) *int { return &s.Preset }),
	CategoryLength:    settingCategory(lengthNames, func(s *Settings) *int { return &s.Bars }),
	CategoryBeatmatch: settingCategory(beatmatchNames, func(s *Settings) *BeatmatchMode { return &s.Beatmatch }),
}

func ParseCategory(name string) (Category, bool) {
	category := Category(name)
	_, ok := categoryValues[category]
	return category, ok
}

func (c Category) Override(style string) map[string]string {
	return map[string]string{string(c): style}
}

func (c Category) IsStyle() bool {
	info, ok := categories[c]
	return ok && info.readStyle != nil
}

func styleCategory[T comparable](names catalogue[T], field func(*Recipe) *T) categoryInfo {
	return categoryInfo{
		names: names.names(),
		readStyle: func(recipe *Recipe) string {
			return names.nameOf(*field(recipe))
		},
		writeStyle: func(recipe *Recipe, name string) {
			if value, ok := names.lookup(name); ok {
				*field(recipe) = value
			}
		},
	}
}

func settingCategory[T comparable](names catalogue[T], field func(*Settings) *T) categoryInfo {
	return categoryInfo{
		names: names.names(),
		writeSetting: func(settings *Settings, name string) {
			if value, ok := names.lookup(name); ok {
				*field(settings) = value
			}
		},
	}
}

var categoryValues = buildCategoryValues()

func buildCategoryValues() map[Category][]string {
	values := make(map[Category][]string, len(categories)+len(legacyStyles))
	for category, info := range categories {
		values[category] = withAuto(info.names)
	}
	for category, styles := range legacyStyles {
		names := make([]string, 0, len(styles))
		for _, style := range styles {
			names = append(names, style.name)
		}
		values[category] = withAuto(names)
	}
	return values
}

func withAuto(names []string) []string {
	values := make([]string, 0, len(names)+1)
	values = append(values, StyleAuto)
	values = append(values, names...)
	return values[:len(values):len(values)]
}

func StyleCategories() []Category {
	return styleCategories[:len(styleCategories):len(styleCategories)]
}

func SettingCategories() []Category {
	return settingCategories[:len(settingCategories):len(settingCategories)]
}

func StyleValues(category Category) []string {
	return categoryValues[category]
}

func ValidStyle(category Category, value string) bool {
	return slices.Contains(categoryValues[category], value)
}

func StyleOf(recipe *Recipe, category Category) string {
	if info, ok := categories[category]; ok && info.readStyle != nil {
		return info.readStyle(recipe)
	}
	return StyleAuto
}

func (r *Recipe) Apply(overrides map[string]string) {
	for _, category := range styleCategories {
		if value, ok := overrides[string(category)]; ok {
			categories[category].writeStyle(r, value)
		}
	}
}

type Resolved struct {
	Recipe  Recipe
	Sources map[Category]string
}

var overrideLayers = [...]string{"guild", "song"}

func ResolveStyles(auto *Recipe, guild, song map[string]string) *Resolved {
	resolved := &Resolved{Recipe: *auto, Sources: make(map[Category]string, len(styleCategories))}
	for _, category := range styleCategories {
		resolved.Sources[category] = "auto"
	}

	for index, overrides := range [...]map[string]string{guild, song} {
		reportUnknownOverrides(overrides, overrideLayers[index])
		for _, category := range styleCategories {
			if value := overrides[string(category)]; value != StyleAuto && ValidStyle(category, value) {
				resolved.Sources[category] = overrideLayers[index]
			}
		}
		resolved.Recipe.Apply(overrides)
	}
	return resolved
}

func reportUnknownOverrides(overrides map[string]string, layer string) {
	for key, value := range overrides {
		if category := Category(key); categories[category].names == nil || !ValidStyle(category, value) {
			logger.Errorf("Ignoring the unknown AutoMix %s override %s=%s", layer, key, value)
		}
	}
}

func ResolveSettings(overrides map[string]string) Settings {
	var settings Settings
	for _, category := range settingCategories {
		if value, ok := overrides[string(category)]; ok {
			categories[category].writeSetting(&settings, value)
		}
	}
	return settings
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

func (r *Resolved) ClampRolls(periodSec float64, frames int) {
	if schedule, ok := loopSchedules[r.Recipe.Loop]; ok && !schedule.fits(periodSec, frames) {
		r.Recipe.Loop = LoopNone
	}
	if schedule, ok := incomingSchedules[r.Recipe.In.FX]; ok && !schedule.fits(periodSec, frames) {
		r.Recipe.In.FX = FXNone
	}
}
