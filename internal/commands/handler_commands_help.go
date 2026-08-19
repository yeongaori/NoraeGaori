package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerHelpCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
		Name:        "help",
		Description: cmd("help").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("help").Options["page"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleHelp,
		TextOnly: false,
		Usage:    cmd("help").Usage,
		Example:  cmd("help").Example,
	})
	registerCommandAliases("help", cmd("help"))
}
