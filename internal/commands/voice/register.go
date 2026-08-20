package voice

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("join", cmd("join"))
	command.RegisterCommand(&command.Command{
		Name:        "leave",
		Description: cmd("leave").Description,
		Handler:     HandleLeave,
		TextOnly:    false,
		Usage:       cmd("leave").Usage,
		Example:     cmd("leave").Example,
	})
	command.RegisterAliases("leave", cmd("leave"))
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("switchvc", cmd("switchvc"))
}
