package playback

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
		Name:        "pause",
		Description: cmd("pause").Description,
		Handler:     HandlePause,
		TextOnly:    false,
		Usage:       cmd("pause").Usage,
		Example:     cmd("pause").Example,
	})
	command.RegisterAliases("pause", cmd("pause"))
	command.RegisterCommand(&command.Command{
		Name:        "resume",
		Description: cmd("resume").Description,
		Handler:     HandleResume,
		TextOnly:    false,
		Usage:       cmd("resume").Usage,
		Example:     cmd("resume").Example,
	})
	command.RegisterAliases("resume", cmd("resume"))
	command.RegisterCommand(&command.Command{
		Name:        "skip",
		Description: cmd("skip").Description,
		Handler:     HandleSkip,
		TextOnly:    false,
		Usage:       cmd("skip").Usage,
		Example:     cmd("skip").Example,
	})
	command.RegisterCommand(&command.Command{
		Name:        "seek",
		Description: cmd("seek").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "position",
				Description: cmd("seek").Options["position"],
				Required:    true,
			},
		},
		Handler:  HandleSeek,
		TextOnly: false,
		Usage:    cmd("seek").Usage,
		Example:  cmd("seek").Example,
	})
	command.RegisterAliases("seek", cmd("seek"))
	command.RegisterCommand(&command.Command{
		Name:        "stop",
		Description: cmd("stop").Description,
		Handler:     HandleStop,
		TextOnly:    false,
		Usage:       cmd("stop").Usage,
		Example:     cmd("stop").Example,
	})
	command.RegisterAliases("stop", cmd("stop"))
	command.RegisterCommand(&command.Command{
		Name:        "nowplaying",
		Description: cmd("nowplaying").Description,
		Handler:     HandleNowPlaying,
		TextOnly:    false,
		Usage:       cmd("nowplaying").Usage,
		Example:     cmd("nowplaying").Example,
	})
	command.RegisterAliases("nowplaying", cmd("nowplaying"))
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("volume", cmd("volume"))
	command.RegisterCommand(&command.Command{
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
	command.RegisterAliases("repeat", cmd("repeat"))
}
