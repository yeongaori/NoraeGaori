package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerAutomixCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
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
	registerCommandAliases("fadein", cmd("fadein"))

	RegisterCommand(&Command{
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
	registerCommandAliases("fadeout", cmd("fadeout"))

	RegisterCommand(&Command{
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
	registerCommandAliases("automix", cmd("automix"))

	RegisterCommand(&Command{
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
	registerCommandAliases("automixstyle", cmd("automixstyle"))

	RegisterCommand(&Command{
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
	registerCommandAliases("automixpanel", cmd("automixpanel"))

	RegisterCommand(&Command{
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
	registerCommandAliases("crossfade", cmd("crossfade"))

	RegisterCommand(&Command{
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
	registerCommandAliases("fadeonstop", cmd("fadeonstop"))

	RegisterCommand(&Command{
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
	registerCommandAliases("trimsilence", cmd("trimsilence"))
}
