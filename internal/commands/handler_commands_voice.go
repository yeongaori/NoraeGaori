package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerVoiceCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
		Name:        "join",
		Description: cmd("join").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "channel",
				Description: cmd("join").Options["channel"],
				Required:    false,
				ChannelTypes: []discordgo.ChannelType{
					discordgo.ChannelTypeGuildVoice,
				},
			},
		},
		Handler:  HandleJoin,
		TextOnly: false,
		Usage:    cmd("join").Usage,
		Example:  cmd("join").Example,
	})
	registerCommandAliases("join", cmd("join"))

	RegisterCommand(&Command{
		Name:        "leave",
		Description: cmd("leave").Description,
		Handler:     HandleLeave,
		TextOnly:    false,
		Usage:       cmd("leave").Usage,
		Example:     cmd("leave").Example,
	})
	registerCommandAliases("leave", cmd("leave"))

	RegisterCommand(&Command{
		Name:        "switchvc",
		Description: cmd("switchvc").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "channel",
				Description: cmd("switchvc").Options["channel"],
				Required:    false,
				ChannelTypes: []discordgo.ChannelType{
					discordgo.ChannelTypeGuildVoice,
				},
			},
		},
		Handler:  HandleSwitchVC,
		TextOnly: false,
		Usage:    cmd("switchvc").Usage,
		Example:  cmd("switchvc").Example,
	})
	registerCommandAliases("switchvc", cmd("switchvc"))
}
