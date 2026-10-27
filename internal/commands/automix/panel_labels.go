package automix

import (
	"fmt"
	"noraegaori/internal/audio/transition"
	"slices"
	"strconv"
	"strings"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func styleLabelKey(category transition.Category, style string) string {
	kind := strings.TrimSuffix(strings.TrimSuffix(string(category), "_out"), "_in")
	return kind + "." + style
}

func lookupLabel(guildID string, labels map[string]string, key, fallback string) string {
	if label := labels[key]; label != "" {
		return label
	}
	logger.Errorf("AutoMix panel label %q is missing from the %s locale", key, messages.Lang(guildID))
	return fallback
}

func styleLabel(guildID string, category transition.Category, style string) string {
	return lookupLabel(guildID, messages.T(guildID).AutoMixPanel.StyleLabels, styleLabelKey(category, style), style)
}

func categoryLabel(guildID string, category transition.Category) string {
	return lookupLabel(guildID, messages.T(guildID).AutoMixPanel.CategoryLabels, string(category), string(category))
}

func MissingLabels(panel *messages.AutoMixPanelMessages) []string {
	var missing []string
	for _, category := range slices.Concat(transition.StyleCategories(), transition.SettingCategories()) {
		if panel.CategoryLabels[string(category)] == "" {
			missing = append(missing, string(category))
		}
		for _, style := range transition.StyleValues(category)[1:] {
			if key := styleLabelKey(category, style); panel.StyleLabels[key] == "" && !slices.Contains(missing, key) {
				missing = append(missing, key)
			}
		}
	}
	return missing
}

func reportMissingLabels() {
	for _, lang := range messages.AvailableLocales() {
		for _, key := range MissingLabels(&messages.ForLang(lang).AutoMixPanel) {
			logger.Errorf("AutoMix panel label %q is missing from the %s locale", key, lang)
		}
	}
}

func tabLabel(guildID, tab string) string {
	panel := &messages.T(guildID).AutoMixPanel
	switch tab {
	case tabOutgoing:
		return panel.OutgoingField
	case tabIncoming:
		return panel.IncomingField
	}
	return panel.SettingsTab
}

func autoValueLabel(guildID string, row *transitionRow, category transition.Category) string {
	if category.IsStyle() {
		return styleLabel(guildID, category, transition.StyleOf(row.auto, category))
	}
	if row.overlap == nil {
		return messages.T(guildID).AutoMixPanel.Unknown
	}
	switch category {
	case transition.CategoryPreset:
		return styleLabel(guildID, category, strconv.Itoa(row.overlap.Preset))
	case transition.CategoryLength:
		return lengthLabel(guildID, row.overlap)
	}
	if row.overlap.Beatmatched {
		return styleLabel(guildID, category, "on")
	}
	return styleLabel(guildID, category, "off")
}

func effectiveValueLabel(guildID string, row *transitionRow, category transition.Category) string {
	if category.IsStyle() {
		return styleLabel(guildID, category, transition.StyleOf(&row.resolved.Recipe, category))
	}
	if value := row.override(category); value != transition.StyleAuto {
		return styleLabel(guildID, category, value)
	}
	return autoValueLabel(guildID, row, category)
}

func lengthLabel(guildID string, overlap *transition.Overlap) string {
	panel := &messages.T(guildID).AutoMixPanel
	if overlap.Bars > 0 {
		return fmt.Sprintf(panel.BarsFormat, overlap.Bars)
	}
	return fmt.Sprintf(panel.SecondsFormat, overlap.Length)
}

func sourceLabel(guildID, source string) string {
	panel := &messages.T(guildID).AutoMixPanel
	switch source {
	case "guild":
		return panel.SourceGuild
	case sourceSong:
		return panel.SourceSong
	}
	return panel.SourceAuto
}
