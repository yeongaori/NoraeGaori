package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
)

func buildSettingsEmbed(view panelView) *discordgo.MessageEmbed {
	panel := panelStrings(view.guildID)

	fields := make([]*discordgo.MessageEmbedField, 0, len(view.specs))
	for _, spec := range view.specs {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   settingLabel(view.guildID, spec.key),
			Value:  view.displayValue(spec),
			Inline: true,
		})
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       fmt.Sprintf("%s - %s", panel.Title, categoryLabel(view.guildID, view.category)),
		Description: panel.Description,
		Fields:      fields,
	}
}
