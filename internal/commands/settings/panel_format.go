package settings

import (
	"errors"
	"fmt"

	"noraegaori/internal/config"
	"noraegaori/internal/messages"
)

func panelStrings(guildID string) messages.SettingsPanelMessages {
	return messages.T(guildID).SettingsPanel
}

func settingLabel(guildID, key string) string {
	if label, ok := panelStrings(guildID).Labels[key]; ok && label != "" {
		return label
	}
	return key
}

func settingHint(guildID, key string) string {
	return panelStrings(guildID).Hints[key]
}

func categoryLabel(guildID, category string) string {
	if label, ok := panelStrings(guildID).Categories[category]; ok && label != "" {
		return label
	}
	return category
}

func defaultFor(spec settingSpec) string {
	cfg := config.GetConfig()
	if cfg == nil {
		return ""
	}
	switch spec.key {
	case "prefix":
		return cfg.Prefix
	case "language":
		return cfg.Language
	default:
		return ""
	}
}

func displayValue(guildID string, spec settingSpec) string {
	panel := panelStrings(guildID)

	value, ok := currentValue(guildID, spec)
	if !ok {
		return panel.ReadFailed
	}

	switch spec.kind {
	case settingToggle:
		return toggleDisplay(guildID, value)
	case settingCycle:
		return repeatDisplay(guildID, value)
	case settingText, settingChoice:
		if value == "" {
			return fmt.Sprintf(panel.DefaultValue, defaultFor(spec))
		}
		return value
	default:
		return value
	}
}

func toggleDisplay(guildID, value string) string {
	settings := messages.T(guildID).Settings
	if value == valueOn {
		return settings.StatusOn
	}
	return settings.StatusOff
}

func repeatDisplay(guildID, value string) string {
	panel := panelStrings(guildID)
	switch value {
	case valueRepeatAll:
		return panel.RepeatAll
	case valueRepeatSingle:
		return panel.RepeatSingle
	default:
		return panel.RepeatOff
	}
}

func validationMessage(guildID string, spec settingSpec, err error) string {
	panel := panelStrings(guildID)
	label := settingLabel(guildID, spec.key)

	switch {
	case errors.Is(err, errNotNumber):
		return fmt.Sprintf(panel.NotNumber, label)
	case errors.Is(err, errOutOfRange):
		return fmt.Sprintf(panel.OutOfRange, label, formatFloat(spec.min), formatFloat(spec.max))
	case errors.Is(err, errTooLong):
		return fmt.Sprintf(panel.TooLong, label, int(spec.max))
	default:
		return fmt.Sprintf(panel.SaveFailed, label, err)
	}
}
