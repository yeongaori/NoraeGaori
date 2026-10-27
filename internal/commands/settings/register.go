package settings

import (
	"strings"

	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name: "setprefix",
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
	})
	setLanguage := cmd("setlanguage")
	for _, langCmd := range []string{"setlanguage", "lang", "language"} {
		name := langCmd
		command.RegisterCommand(&command.Command{
			Name:        name,
			Description: setLanguage.Description,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "language",
					Description: setLanguage.Options["language"],
					Required:    false,
					Choices:     CreateLanguageChoices(),
				},
			},
			Handler:   HandleSetLanguage,
			AdminOnly: true,
			TextOnly:  true,
			Usage:     setLanguage.Usage,
			Example:   setLanguage.Example,
		})
	}
	command.RegisterAliases("setlanguage", &setLanguage)
	for _, name := range []string{"sponsorblock", "showstartedtrack", "normalization", "autoleave", "autopause", "autoresume"} {
		RegisterToggle(cmd, name)
	}
	command.RegisterLocalized(cmd, &command.Command{
		Name: "settings",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "category",
				Description: cmd("settings").Options["category"],
				Required:    false,
				Choices:     CreateCategoryChoices(),
			},
		},
		Handler: HandleSettingsPanel,
	})
	registerSettingMenus()
	registerPanelRoutes()
}

func RegisterToggle(cmd func(string) messages.CommandStrings, name string) {
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
		options = append(options, numberOption(&commandStrings, strings.TrimPrefix(number.key, name+"_"), number))
	}

	command.RegisterLocalized(cmd, &command.Command{
		Name:    name,
		Options: options,
		Handler: HandleSetting(name),
	})
}

func numberOption(commandStrings *messages.CommandStrings, name string, spec *settingSpec) *discordgo.ApplicationCommandOption {
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
