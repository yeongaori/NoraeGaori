package settings

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
)

const (
	categoryRoute = "settings_category"
	pickRoute     = "settings_pick"
	modalRoute    = "settings_modal"

	selectOptionLimit = 25
	optionLabelLimit  = 100
)

func visibleCategories(isAdmin bool) []string {
	visible := make([]string, 0, len(settingCategories))
	for _, category := range settingCategories {
		if hasVisibleSetting(category, isAdmin) {
			visible = append(visible, category)
		}
	}
	return visible
}

func defaultCategory(isAdmin bool) string {
	for _, category := range settingCategories {
		if hasVisibleSetting(category, isAdmin) {
			return category
		}
	}
	return categoryPlayback
}

func buildSettingsComponents(view *panelView) []discordgo.MessageComponent {
	components := make([]discordgo.MessageComponent, 0, 2)
	components = append(components, categoryRow(view))
	if len(view.specs) > 0 {
		components = append(components, settingRow(view))
	}
	return components
}

func categoryRow(view *panelView) discordgo.ActionsRow {
	options := make([]discordgo.SelectMenuOption, 0, len(settingCategories))
	for _, name := range settingCategories {
		if !hasVisibleSetting(name, view.isAdmin) {
			continue
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:   categoryLabel(view.guildID, name),
			Value:   name,
			Default: name == view.category,
		})
	}

	return selectRow(discordgo.SelectMenu{
		CustomID:    discord.ComponentID(categoryRoute, discord.ViewArgument(view.isAdmin)),
		Placeholder: panelStrings(view.guildID).CategoryPlaceholder,
		Options:     options,
	})
}

func settingRow(view *panelView) discordgo.ActionsRow {
	options := make([]discordgo.SelectMenuOption, 0, min(len(view.specs), selectOptionLimit))
	for _, spec := range view.specs {
		if len(options) == selectOptionLimit {
			break
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       discord.TruncateRunes(settingLabel(view.guildID, spec.key), optionLabelLimit),
			Description: discord.TruncateRunes(view.displayValue(spec), optionLabelLimit),
			Value:       spec.key,
		})
	}

	return selectRow(discordgo.SelectMenu{
		CustomID:    discord.ComponentID(pickRoute, discord.ViewArgument(view.isAdmin), view.category),
		Placeholder: panelStrings(view.guildID).SettingPlaceholder,
		Options:     options,
	})
}

func selectRow(menu discordgo.SelectMenu) discordgo.ActionsRow {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{menu}}
}
