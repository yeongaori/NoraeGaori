package voice

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "join",
		Options: []*discordgo.ApplicationCommandOption{command.VoiceChannelOption(cmd("join").Options["channel"])},
		Handler: HandleJoin,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "leave",
		Handler: HandleLeave,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "switchvc",
		Options: []*discordgo.ApplicationCommandOption{command.VoiceChannelOption(cmd("switchvc").Options["channel"])},
		Handler: HandleSwitchVC,
	})
}
