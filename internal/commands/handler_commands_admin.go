package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerAdminCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
		Name:        "setprefix",
		Description: cmd("setprefix").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "prefix",
				Description: cmd("setprefix").Options["prefix"],
				Required:    false,
			},
		},
		Handler:   HandleSetPrefix,
		AdminOnly: true,
		TextOnly:  true,
		Usage:     cmd("setprefix").Usage,
		Example:   cmd("setprefix").Example,
	})
	registerCommandAliases("setprefix", cmd("setprefix"))

	for _, langCmd := range []string{"setlanguage", "lang", "language"} {
		name := langCmd
		RegisterCommand(&Command{
			Name:        name,
			Description: cmd("setlanguage").Description,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "language",
					Description: cmd("setlanguage").Options["language"],
					Required:    false,
					Choices:     buildLanguageChoices(),
				},
			},
			Handler:   HandleSetLanguage,
			AdminOnly: true,
			TextOnly:  true,
			Usage:     cmd("setlanguage").Usage,
			Example:   cmd("setlanguage").Example,
		})
	}
	registerCommandAliases("setlanguage", cmd("setlanguage"))

	RegisterCommand(&Command{
		Name:        "forceskip",
		Description: cmd("forceskip").Description,
		Handler:     HandleForceSkip,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("forceskip").Usage,
		Example:     cmd("forceskip").Example,
	})
	registerCommandAliases("forceskip", cmd("forceskip"))

	RegisterCommand(&Command{
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
	registerCommandAliases("forceremove", cmd("forceremove"))

	RegisterCommand(&Command{
		Name:        "forcestop",
		Description: cmd("forcestop").Description,
		Handler:     HandleForceStop,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("forcestop").Usage,
		Example:     cmd("forcestop").Example,
	})
	registerCommandAliases("forcestop", cmd("forcestop"))

	RegisterCommand(&Command{
		Name:        "movetrack",
		Description: cmd("movetrack").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "from",
				Description: cmd("movetrack").Options["from"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "to",
				Description: cmd("movetrack").Options["to"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:   HandleMoveTrack,
		AdminOnly: true,
		TextOnly:  true,
		Usage:     cmd("movetrack").Usage,
		Example:   cmd("movetrack").Example,
	})
	registerCommandAliases("movetrack", cmd("movetrack"))

	RegisterCommand(&Command{
		Name:        "status",
		Description: cmd("status").Description,
		Handler:     HandleStatus,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("status").Usage,
		Example:     cmd("status").Example,
	})
}
