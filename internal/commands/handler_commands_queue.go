package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerQueueCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
		Name:        "nowplaying",
		Description: cmd("nowplaying").Description,
		Handler:     HandleNowPlaying,
		TextOnly:    false,
		Usage:       cmd("nowplaying").Usage,
		Example:     cmd("nowplaying").Example,
	})
	registerCommandAliases("nowplaying", cmd("nowplaying"))

	RegisterCommand(&Command{
		Name:        "volume",
		Description: cmd("volume").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionNumber,
				Name:        "level",
				Description: cmd("volume").Options["level"],
				Required:    false,
				MinValue:    func() *float64 { v := 0.0; return &v }(),
				MaxValue:    1000.0,
			},
		},
		Handler:  HandleVolume,
		TextOnly: false,
		Usage:    cmd("volume").Usage,
		Example:  cmd("volume").Example,
	})
	registerCommandAliases("volume", cmd("volume"))

	RegisterCommand(&Command{
		Name:        "repeat",
		Description: cmd("repeat").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "mode",
				Description: cmd("repeat").Options["mode"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "single", Value: "single"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleRepeat,
		TextOnly: false,
		Usage:    cmd("repeat").Usage,
		Example:  cmd("repeat").Example,
	})
	registerCommandAliases("repeat", cmd("repeat"))

	RegisterCommand(&Command{
		Name:        "queue",
		Description: cmd("queue").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("queue").Options["page"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleQueue,
		TextOnly: false,
		Usage:    cmd("queue").Usage,
		Example:  cmd("queue").Example,
	})
	registerCommandAliases("queue", cmd("queue"))

	RegisterCommand(&Command{
		Name:        "remove",
		Description: cmd("remove").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "position",
				Description: cmd("remove").Options["position"],
				Required:    true,
			},
		},
		Handler:  HandleRemove,
		TextOnly: false,
		Usage:    cmd("remove").Usage,
		Example:  cmd("remove").Example,
	})
	registerCommandAliases("remove", cmd("remove"))

	RegisterCommand(&Command{
		Name:        "swap",
		Description: cmd("swap").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "position1",
				Description: cmd("swap").Options["position1"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "position2",
				Description: cmd("swap").Options["position2"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleSwap,
		TextOnly: false,
		Usage:    cmd("swap").Usage,
		Example:  cmd("swap").Example,
	})

	RegisterCommand(&Command{
		Name:        "skipto",
		Description: cmd("skipto").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "position",
				Description: cmd("skipto").Options["position"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleSkipTo,
		TextOnly: false,
		Usage:    cmd("skipto").Usage,
		Example:  cmd("skipto").Example,
	})
	registerCommandAliases("skipto", cmd("skipto"))
}
