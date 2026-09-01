package settings

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
)

const (
	categoryPrefix = "settings_cat_"
	pickPrefix     = "settings_pick_"
	choicePrefix   = "settings_choice_"
	modalPrefix    = "settings_modal_"
	inputPrefix    = "settings_input_"

	selectOptionLimit = 25
	optionLabelLimit  = 100
)

func customID(prefix, key, token string) string {
	return prefix + key + "_" + token
}

func settingKeyFrom(customID, prefix, token string) string {
	return strings.TrimSuffix(strings.TrimPrefix(customID, prefix), "_"+token)
}

func visibleCategories(isAdmin bool) []string {
	visible := make([]string, 0, len(settingCategories))
	for _, category := range settingCategories {
		if len(settingsInCategory(category, isAdmin)) > 0 {
			visible = append(visible, category)
		}
	}
	return visible
}

func defaultCategory(isAdmin bool) string {
	if visible := visibleCategories(isAdmin); len(visible) > 0 {
		return visible[0]
	}
	return categoryPlayback
}

func buildSettingsComponents(view panelView) []discordgo.MessageComponent {
	components := []discordgo.MessageComponent{categoryRow(view)}

	if spec, found := openChoice(view.specs, view.openSetting); found {
		return append(components, choiceRow(view, spec))
	}

	if len(view.specs) > 0 {
		components = append(components, settingRow(view))
	}

	return components
}

func openChoice(specs []settingSpec, openSetting string) (settingSpec, bool) {
	if openSetting == "" {
		return settingSpec{}, false
	}
	for _, spec := range specs {
		if spec.key == openSetting && spec.kind == settingChoice {
			return spec, true
		}
	}
	return settingSpec{}, false
}

func categoryRow(view panelView) discordgo.ActionsRow {
	visible := visibleCategories(view.isAdmin)
	options := make([]discordgo.SelectMenuOption, 0, len(visible))
	for _, name := range visible {
		options = append(options, discordgo.SelectMenuOption{
			Label:   categoryLabel(view.guildID, name),
			Value:   name,
			Default: name == view.category,
		})
	}

	return discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				CustomID:    categoryPrefix + view.token,
				Placeholder: panelStrings(view.guildID).CategoryPlaceholder,
				Options:     options,
			},
		},
	}
}

func settingRow(view panelView) discordgo.ActionsRow {
	options := make([]discordgo.SelectMenuOption, 0, len(view.specs))
	for _, spec := range view.specs {
		if len(options) >= selectOptionLimit {
			break
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(settingLabel(view.guildID, spec.key), optionLabelLimit),
			Description: discord.TruncateRunes(view.displayValue(spec), optionLabelLimit),
			Value:       spec.key,
		})
	}

	return discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				CustomID:    pickPrefix + view.token,
				Placeholder: panelStrings(view.guildID).SettingPlaceholder,
				Options:     options,
			},
		},
	}
}

func choiceRow(view panelView, spec settingSpec) discordgo.ActionsRow {
	panel := panelStrings(view.guildID)
	current := view.values[spec.key].raw

	values := spec.options()
	options := make([]discordgo.SelectMenuOption, 0, len(values)+2)
	options = append(options,
		discordgo.SelectMenuOption{Label: panel.BackOption, Value: backValue},
		discordgo.SelectMenuOption{
			Label:   panel.DefaultOption,
			Value:   defaultChoiceValue,
			Default: current == "",
		},
	)
	for _, value := range values {
		if len(options) >= selectOptionLimit {
			break
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:   discord.TruncateRunes(value, optionLabelLimit),
			Value:   value,
			Default: value == current,
		})
	}

	return discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				CustomID:    customID(choicePrefix, spec.key, view.token),
				Placeholder: fmt.Sprintf(panel.ChoicePlaceholder, settingLabel(view.guildID, spec.key)),
				Options:     options,
			},
		},
	}
}
