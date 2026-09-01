package settings

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
)

func BuildLanguageChoices() []*discordgo.ApplicationCommandOptionChoice {
	codes := messages.AvailableLocales()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(codes))
	for _, code := range codes {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  code,
			Value: code,
		})
	}
	return choices
}
