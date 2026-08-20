package automix

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
		Name:        "fadein",
		Description: cmd("fadein").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("fadein").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: cmd("fadein").Options["duration"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
				MaxValue:    30.0,
			},
		},
		Handler:  HandleFadeIn,
		TextOnly: false,
		Usage:    cmd("fadein").Usage,
		Example:  cmd("fadein").Example,
	})
	command.RegisterAliases("fadein", cmd("fadein"))
	command.RegisterCommand(&command.Command{
		Name:        "fadeout",
		Description: cmd("fadeout").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("fadeout").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: cmd("fadeout").Options["duration"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
				MaxValue:    30.0,
			},
		},
		Handler:  HandleFadeOut,
		TextOnly: false,
		Usage:    cmd("fadeout").Usage,
		Example:  cmd("fadeout").Example,
	})
	command.RegisterAliases("fadeout", cmd("fadeout"))
	command.RegisterCommand(&command.Command{
		Name:        "automix",
		Description: cmd("automix").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("automix").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "beats",
				Description: cmd("automix").Options["beats"],
				Required:    false,
				MinValue:    func() *float64 { v := 4.0; return &v }(),
				MaxValue:    64.0,
			},
		},
		Handler:  HandleAutoMix,
		TextOnly: false,
		Usage:    cmd("automix").Usage,
		Example:  cmd("automix").Example,
	})
	command.RegisterAliases("automix", cmd("automix"))
	command.RegisterCommand(&command.Command{
		Name:        "automixstyle",
		Description: cmd("automixstyle").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "category",
				Description: cmd("automixstyle").Options["category"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "volume", Value: "volume"},
					{Name: "eq", Value: "eq"},
					{Name: "filter", Value: "filter"},
					{Name: "effect", Value: "effect"},
					{Name: "loop", Value: "loop"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "style",
				Description: cmd("automixstyle").Options["style"],
				Required:    false,
			},
		},
		Handler:  HandleAutoMixStyle,
		TextOnly: false,
		Usage:    cmd("automixstyle").Usage,
		Example:  cmd("automixstyle").Example,
	})
	command.RegisterAliases("automixstyle", cmd("automixstyle"))
	command.RegisterCommand(&command.Command{
		Name:        "automixpanel",
		Description: cmd("automixpanel").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("automixpanel").Options["page"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleAutoMixPanel,
		TextOnly: false,
		Usage:    cmd("automixpanel").Usage,
		Example:  cmd("automixpanel").Example,
	})
	command.RegisterAliases("automixpanel", cmd("automixpanel"))
	command.RegisterCommand(&command.Command{
		Name:        "crossfade",
		Description: cmd("crossfade").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("crossfade").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: cmd("crossfade").Options["duration"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
				MaxValue:    30.0,
			},
		},
		Handler:  HandleCrossfade,
		TextOnly: false,
		Usage:    cmd("crossfade").Usage,
		Example:  cmd("crossfade").Example,
	})
	command.RegisterAliases("crossfade", cmd("crossfade"))
	command.RegisterCommand(&command.Command{
		Name:        "fadeonstop",
		Description: cmd("fadeonstop").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("fadeonstop").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleFadeOnStop,
		TextOnly: false,
		Usage:    cmd("fadeonstop").Usage,
		Example:  cmd("fadeonstop").Example,
	})
	command.RegisterAliases("fadeonstop", cmd("fadeonstop"))
	command.RegisterCommand(&command.Command{
		Name:        "trimsilence",
		Description: cmd("trimsilence").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("trimsilence").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleTrimSilence,
		TextOnly: false,
		Usage:    cmd("trimsilence").Usage,
		Example:  cmd("trimsilence").Example,
	})
	command.RegisterAliases("trimsilence", cmd("trimsilence"))
}
