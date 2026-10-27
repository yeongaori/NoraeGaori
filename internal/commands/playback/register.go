package playback

import (
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	registerPlaybackControls(cmd)
	registerSoundSettings(cmd)
}

func registerPlaybackControls(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "pause",
		Handler: HandlePause,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "resume",
		Handler: HandleResume,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "skip",
		Handler: HandleSkip,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name: "seek",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "position",
				Description: cmd("seek").Options["position"],
				Required:    true,
			},
		},
		Handler: HandleSeek,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "stop",
		Handler: HandleStop,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:    "nowplaying",
		Handler: HandleNowPlaying,
	})
}

func registerSoundSettings(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name: "volume",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionNumber,
				Name:        "level",
				Description: cmd("volume").Options["level"],
				Required:    false,
				MinValue:    command.FloatPointer(0),
				MaxValue:    1000.0,
			},
		},
		Handler: HandleVolume,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name: "repeat",
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
		Handler: settings.HandleSetting("repeat"),
	})
}
