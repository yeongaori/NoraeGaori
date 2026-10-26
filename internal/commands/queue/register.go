package queue

import (
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name: "queue",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("queue").Options["page"],
				Required:    false,
				MinValue:    command.FloatPointer(1),
			},
		},
		Handler: HandleQueue,
	})
	discord.RegisterComponentRoute(queuePageRoute, turnQueuePage)
	discord.RegisterComponentRoute(queueMixRoute, openMixPanel)
	command.RegisterLocalized(cmd, &command.Command{
		Name: "remove",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "position",
				Description: cmd("remove").Options["position"],
				Required:    true,
			},
		},
		Handler: HandleRemove,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name: "swap",
		Options: []*discordgo.ApplicationCommandOption{
			command.PositionOption("position1", cmd("swap").Options["position1"]),
			command.PositionOption("position2", cmd("swap").Options["position2"]),
		},
		Handler: HandleSwap,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "skipto",
		Options: []*discordgo.ApplicationCommandOption{command.PositionOption("position", cmd("skipto").Options["position"])},
		Handler: HandleSkipTo,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name: "movetrack",
		Options: []*discordgo.ApplicationCommandOption{
			command.PositionOption("from", cmd("movetrack").Options["from"]),
			command.PositionOption("to", cmd("movetrack").Options["to"]),
		},
		Handler:   HandleMoveTrack,
		AdminOnly: true,
		TextOnly:  true,
	})
}
