package automix

import (
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	for _, name := range []string{"fadein", "fadeout", "automix", "crossfade", "fadeonstop", "trimsilence"} {
		settings.RegisterToggleCommand(cmd, name)
	}
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
	registerPanelRoutes()
}
