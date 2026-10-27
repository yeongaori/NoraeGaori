package help

import (
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name: "help",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("help").Options["page"],
				Required:    false,
				MinValue:    command.FloatPointer(1),
			},
		},
		Handler: HandleHelp,
	})
	discord.RegisterRoute(helpPageRoute, turnHelpPage)
}
