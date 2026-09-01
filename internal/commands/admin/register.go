package admin

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
		Name:        "forceskip",
		Description: cmd("forceskip").Description,
		Handler:     HandleForceSkip,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("forceskip").Usage,
		Example:     cmd("forceskip").Example,
	})
	command.RegisterAliases("forceskip", cmd("forceskip"))
	command.RegisterCommand(&command.Command{
		Name:        "forceremove",
		Description: cmd("forceremove").Description,
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
		Usage:     cmd("forceremove").Usage,
		Example:   cmd("forceremove").Example,
	})
	command.RegisterAliases("forceremove", cmd("forceremove"))
	command.RegisterCommand(&command.Command{
		Name:        "forcestop",
		Description: cmd("forcestop").Description,
		Handler:     HandleForceStop,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("forcestop").Usage,
		Example:     cmd("forcestop").Example,
	})
	command.RegisterAliases("forcestop", cmd("forcestop"))
	command.RegisterCommand(&command.Command{
		Name:        "status",
		Description: cmd("status").Description,
		Handler:     HandleStatus,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("status").Usage,
		Example:     cmd("status").Example,
	})
}
