package settings

import (
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

const (
	categoryGeneral  = "general"
	categoryPlayback = "playback"
	categoryMixing   = "mixing"

	maxPrefixLength = 5
)

var settingCategories = []string{categoryGeneral, categoryPlayback, categoryMixing}

type settingKind int

const (
	settingToggle settingKind = iota
	settingCycle
	settingText
	settingNumber
	settingChoice
)

type settingSpec struct {
	key       string
	category  string
	kind      settingKind
	adminOnly bool
	isInteger bool
	min       float64
	max       float64
	options   func() []string
	read      func(guildID string) (string, error)
	write     func(guildID, value string) error
}

var settingSpecs = []settingSpec{
	{
		key:       "prefix",
		category:  categoryGeneral,
		kind:      settingText,
		adminOnly: true,
		max:       maxPrefixLength,
		read:      readPrefix,
		write:     writePrefix,
	},
	{
		key:       "language",
		category:  categoryGeneral,
		kind:      settingChoice,
		adminOnly: true,
		options:   messages.AvailableLocales,
		read:      readLanguage,
		write:     writeLanguage,
	},
	{
		key:      "volume",
		category: categoryPlayback,
		kind:     settingNumber,
		max:      1000,
		read:     readVolume,
		write:    writeVolume,
	},
	{
		key:      "repeat",
		category: categoryPlayback,
		kind:     settingCycle,
		read:     readRepeat,
		write:    writeRepeat,
	},
	{
		key:      "sponsorblock",
		category: categoryPlayback,
		kind:     settingToggle,
		read:     boolReader(queue.GetSponsorBlock),
		write:    boolWriter(queue.SetSponsorBlock),
	},
	{
		key:      "normalization",
		category: categoryPlayback,
		kind:     settingToggle,
		read:     boolReader(queue.GetNormalization),
		write:    boolWriter(queue.SetNormalization),
	},
	{
		key:      "showstartedtrack",
		category: categoryPlayback,
		kind:     settingToggle,
		read:     boolReader(queue.GetShowStartedTrack),
		write:    boolWriter(queue.SetShowStartedTrack),
	},
	{
		key:      "fadein",
		category: categoryMixing,
		kind:     settingToggle,
		read:     boolReader(queue.GetFadeIn),
		write:    boolWriter(queue.SetFadeIn),
	},
	{
		key:      "fadein_duration",
		category: categoryMixing,
		kind:     settingNumber,
		min:      1,
		max:      30,
		read:     floatReader(queue.GetFadeInDuration),
		write:    floatWriter(queue.SetFadeInDuration),
	},
	{
		key:      "fadeout",
		category: categoryMixing,
		kind:     settingToggle,
		read:     boolReader(queue.GetFadeOut),
		write:    boolWriter(queue.SetFadeOut),
	},
	{
		key:      "fadeout_duration",
		category: categoryMixing,
		kind:     settingNumber,
		min:      1,
		max:      30,
		read:     floatReader(queue.GetFadeOutDuration),
		write:    floatWriter(queue.SetFadeOutDuration),
	},
	{
		key:      "crossfade",
		category: categoryMixing,
		kind:     settingToggle,
		read:     boolReader(queue.GetCrossfade),
		write:    boolWriter(queue.SetCrossfade),
	},
	{
		key:      "crossfade_duration",
		category: categoryMixing,
		kind:     settingNumber,
		min:      1,
		max:      30,
		read:     floatReader(queue.GetCrossfadeDuration),
		write:    floatWriter(queue.SetCrossfadeDuration),
	},
	{
		key:      "automix",
		category: categoryMixing,
		kind:     settingToggle,
		read:     boolReader(queue.GetAutoMix),
		write:    boolWriter(queue.SetAutoMix),
	},
	{
		key:       "automix_beats",
		category:  categoryMixing,
		kind:      settingNumber,
		isInteger: true,
		min:       4,
		max:       64,
		read:      readAutoMixBeats,
		write:     writeAutoMixBeats,
	},
	{
		key:      "fadeonstop",
		category: categoryMixing,
		kind:     settingToggle,
		read:     boolReader(queue.GetFadeOnStop),
		write:    boolWriter(queue.SetFadeOnStop),
	},
	{
		key:      "trimsilence",
		category: categoryMixing,
		kind:     settingToggle,
		read:     boolReader(queue.GetTrimSilence),
		write:    boolWriter(queue.SetTrimSilence),
	},
}

func findSetting(key string) (settingSpec, bool) {
	for _, spec := range settingSpecs {
		if spec.key == key {
			return spec, true
		}
	}
	return settingSpec{}, false
}

func settingsInCategory(category string, isAdmin bool) []settingSpec {
	visible := make([]settingSpec, 0, len(settingSpecs))
	for _, spec := range settingSpecs {
		if spec.category != category {
			continue
		}
		if spec.adminOnly && !isAdmin {
			continue
		}
		visible = append(visible, spec)
	}
	return visible
}

func isKnownCategory(category string) bool {
	for _, known := range settingCategories {
		if known == category {
			return true
		}
	}
	return false
}
