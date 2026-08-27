package settings

import (
	"github.com/bwmarrin/discordgo"
)

func BuildCategoryChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(settingCategories))
	for _, category := range settingCategories {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  categoryLabel("", category),
			Value: category,
		})
	}
	return choices
}
