package settings

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
		Name:        "setprefix",
		Description: cmd("setprefix").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "prefix",
				Description: cmd("setprefix").Options["prefix"],
				Required:    false,
			},
		},
		Handler:   HandleSetPrefix,
		AdminOnly: true,
		TextOnly:  true,
		Usage:     cmd("setprefix").Usage,
		Example:   cmd("setprefix").Example,
	})
	command.RegisterAliases("setprefix", cmd("setprefix"))
	for _, langCmd := range []string{"setlanguage", "lang", "language"} {
		name := langCmd
		command.RegisterCommand(&command.Command{
			Name:        name,
			Description: cmd("setlanguage").Description,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "language",
					Description: cmd("setlanguage").Options["language"],
					Required:    false,
					Choices:     BuildLanguageChoices(),
				},
			},
			Handler:   HandleSetLanguage,
			AdminOnly: true,
			TextOnly:  true,
			Usage:     cmd("setlanguage").Usage,
			Example:   cmd("setlanguage").Example,
		})
	}
	command.RegisterAliases("setlanguage", cmd("setlanguage"))
	command.RegisterCommand(&command.Command{
		Name:        "sponsorblock",
		Description: cmd("sponsorblock").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("sponsorblock").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleSponsorBlock,
		TextOnly: false,
		Usage:    cmd("sponsorblock").Usage,
		Example:  cmd("sponsorblock").Example,
	})
	command.RegisterAliases("sponsorblock", cmd("sponsorblock"))
	command.RegisterCommand(&command.Command{
		Name:        "showstartedtrack",
		Description: cmd("showstartedtrack").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("showstartedtrack").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleShowStartedTrack,
		TextOnly: false,
		Usage:    cmd("showstartedtrack").Usage,
		Example:  cmd("showstartedtrack").Example,
	})
	command.RegisterAliases("showstartedtrack", cmd("showstartedtrack"))
	command.RegisterCommand(&command.Command{
		Name:        "normalization",
		Description: cmd("normalization").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("normalization").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleNormalization,
		TextOnly: false,
		Usage:    cmd("normalization").Usage,
		Example:  cmd("normalization").Example,
	})
	command.RegisterAliases("normalization", cmd("normalization"))
}
