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

type settingValue struct {
	raw string
	ok  bool
}

type panelView struct {
	guildID     string
	category    string
	openSetting string
	token       string
	isAdmin     bool
	specs       []settingSpec
	values      map[string]settingValue
}

func newPanelView(guildID, category, openSetting, token string, isAdmin bool) panelView {
	specs := settingsInCategory(category, isAdmin)
	values := make(map[string]settingValue, len(specs))
	for _, spec := range specs {
		raw, ok := currentValue(guildID, spec)
		values[spec.key] = settingValue{raw: raw, ok: ok}
	}

	return panelView{
		guildID:     guildID,
		category:    category,
		openSetting: openSetting,
		token:       token,
		isAdmin:     isAdmin,
		specs:       specs,
		values:      values,
	}
}

func (view panelView) displayValue(spec settingSpec) string {
	panel := panelStrings(view.guildID)

	value := view.values[spec.key]
	if !value.ok {
		return panel.ReadFailed
	}

	switch spec.kind {
	case settingToggle:
		return toggleDisplay(view.guildID, value.raw)
	case settingCycle:
		return repeatDisplay(view.guildID, value.raw)
	case settingText, settingChoice:
		if value.raw == "" {
			return fmt.Sprintf(panel.DefaultValue, defaultFor(spec))
		}
		return value.raw
	default:
		return value.raw
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
	case errors.Is(err, errNotInteger):
		return fmt.Sprintf(panel.NotInteger, label)
	case errors.Is(err, errOutOfRange):
		return fmt.Sprintf(panel.OutOfRange, label, formatFloat(spec.min), formatFloat(spec.max))
	case errors.Is(err, errTooLong):
		return fmt.Sprintf(panel.TooLong, label, int(spec.max))
	default:
		return fmt.Sprintf(panel.SaveFailed, label, err)
	}
}
