package admin

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name:      "forceskip",
		Handler:   HandleForceSkip,
		AdminOnly: true,
		TextOnly:  true,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name: "forceremove",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "target",
				Description: cmd("forceremove").Options["target"],
				Required:    true,
			},
		},
		Handler:   HandleForceRemove,
		AdminOnly: true,
		TextOnly:  true,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:      "forcestop",
		Handler:   HandleForceStop,
		AdminOnly: true,
		TextOnly:  true,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:      "status",
		Handler:   HandleStatus,
		AdminOnly: true,
		TextOnly:  true,
	})
}
