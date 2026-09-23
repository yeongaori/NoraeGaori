package settings

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
)

func registerSettingMenus() {
	for index := range settingSpecs {
		spec := &settingSpecs[index]
		if !hasSettingMenu(spec) {
			continue
		}
		discord.RegisterDropdownMenu(spec.key, func(guildID string) discord.DropdownMenu {
			return buildSettingMenu(guildID, spec)
		})
	}
}

func hasSettingMenu(spec *settingSpec) bool {
	return !spec.adminOnly && (spec.kind == settingToggle || spec.kind == settingChoice)
}

func settingMenuValues(spec *settingSpec) []string {
	if spec.kind == settingChoice {
		return spec.options()
	}
	return toggleValues
}

func buildSettingMenu(guildID string, spec *settingSpec) discord.DropdownMenu {
	current, isRead := currentValue(guildID, spec)

	values := settingMenuValues(spec)
	options := make([]discordgo.SelectMenuOption, 0, len(values))
	for _, value := range values {
		options = append(options, discordgo.SelectMenuOption{
			Label:   formatSettingValue(guildID, spec, value),
			Value:   value,
			Default: isRead && value == current,
		})
	}

	display := panelStrings(guildID).ReadFailed
	if isRead {
		display = formatSettingValue(guildID, spec, current)
	}

	return discord.DropdownMenu{
		Label:   settingLabel(guildID, spec.key),
		Current: display,
		Options: options,
		Apply: func(value string) (*discordgo.MessageEmbed, error) {
			if err := applySetting(guildID, spec, value); err != nil {
				return messages.CreateErrorEmbed(messages.T(guildID).Titles.Error, validationMessage(guildID, spec, err)), err
			}
			return nil, nil
		},
	}
}
