package queue

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("queue", cmd("queue"))
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("remove", cmd("remove"))
	command.RegisterCommand(&command.Command{
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
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("skipto", cmd("skipto"))
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("movetrack", cmd("movetrack"))
}
