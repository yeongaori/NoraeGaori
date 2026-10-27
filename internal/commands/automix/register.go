package automix

import (
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func categoryChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(commandCategories))
	for _, category := range commandCategories {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: string(category), Value: string(category)})
	}
	return choices
}

func Register(cmd func(string) messages.CommandStrings) {
	for _, name := range []string{"fadein", "fadeout", "automix", "crossfade", "fadeonstop", "trimsilence"} {
		settings.RegisterToggle(cmd, name)
	}
	command.RegisterLocalized(cmd, &command.Command{
		Name: "automixstyle",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "category",
				Description: cmd("automixstyle").Options["category"],
				Required:    false,
				Choices:     categoryChoices(),
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "style",
				Description: cmd("automixstyle").Options["style"],
				Required:    false,
			},
		},
		Handler: HandleAutoMixStyle,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name: "automixpanel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("automixpanel").Options["page"],
				Required:    false,
				MinValue:    command.FloatPointer(1),
			},
		},
		Handler: HandleAutoMixPanel,
	})
	registerPanelRoutes()
	reportMissingLabels()
}
