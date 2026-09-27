package settings

import (
	"strings"

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
	for _, name := range []string{"sponsorblock", "showstartedtrack", "normalization"} {
		RegisterToggleCommand(cmd, name)
	}
	command.RegisterCommand(&command.Command{
		Name:        "settings",
		Description: cmd("settings").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "category",
				Description: cmd("settings").Options["category"],
				Required:    false,
				Choices:     BuildCategoryChoices(),
			},
		},
		Handler:  HandleSettingsPanel,
		TextOnly: false,
		Usage:    cmd("settings").Usage,
		Example:  cmd("settings").Example,
	})
	command.RegisterAliases("settings", cmd("settings"))
	registerSettingMenus()
	registerPanelRoutes()
}

func RegisterToggleCommand(cmd func(string) messages.CommandStrings, name string) {
	commandStrings := cmd(name)
	options := []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "setting",
			Description: commandStrings.Options["setting"],
			Required:    false,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: valueOn, Value: valueOn},
				{Name: valueOff, Value: valueOff},
			},
		},
	}
	if number := findNumberSetting(name, ""); number != nil {
		options = append(options, numberOption(commandStrings, strings.TrimPrefix(number.key, name+"_"), number))
	}

	command.RegisterCommand(&command.Command{
		Name:        name,
		Description: commandStrings.Description,
		Options:     options,
		Handler:     HandleSetting(name),
		TextOnly:    false,
		Usage:       commandStrings.Usage,
		Example:     commandStrings.Example,
	})
	command.RegisterAliases(name, commandStrings)
}

func numberOption(commandStrings messages.CommandStrings, name string, spec *settingSpec) *discordgo.ApplicationCommandOption {
	minValue := spec.min
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionInteger,
		Name:        name,
		Description: commandStrings.Options[name],
		Required:    false,
		MinValue:    &minValue,
		MaxValue:    spec.max,
	}
}
