package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
)

func buildSettingsEmbed(guildID, category string, isAdmin bool) *discordgo.MessageEmbed {
	panel := panelStrings(guildID)

	specs := settingsInCategory(category, isAdmin)
	fields := make([]*discordgo.MessageEmbedField, 0, len(specs))
	for _, spec := range specs {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   settingLabel(guildID, spec.key),
			Value:  displayValue(guildID, spec),
			Inline: true,
		})
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       fmt.Sprintf("%s - %s", panel.Title, categoryLabel(guildID, category)),
		Description: panel.Description,
		Fields:      fields,
	}
}
