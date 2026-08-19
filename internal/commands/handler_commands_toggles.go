package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerTogglesCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
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
	registerCommandAliases("sponsorblock", cmd("sponsorblock"))

	RegisterCommand(&Command{
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
	registerCommandAliases("showstartedtrack", cmd("showstartedtrack"))

	RegisterCommand(&Command{
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
	registerCommandAliases("normalization", cmd("normalization"))
}
