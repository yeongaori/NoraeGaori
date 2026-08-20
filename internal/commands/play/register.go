package play

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterCommand(&command.Command{
		Name:        "play",
		Description: cmd("play").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "query",
				Description:  cmd("play").Options["query"],
				Required:     true,
				Autocomplete: true,
			},
		},
		Handler:             HandlePlay,
		AutocompleteHandler: autocompleteVideoResults,
		TextOnly:            false,
		Usage:               cmd("play").Usage,
		Example:             cmd("play").Example,
	})
	command.RegisterAliases("play", cmd("play"))
	command.RegisterCommand(&command.Command{
		Name:        "playnext",
		Description: cmd("playnext").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "query",
				Description:  cmd("playnext").Options["query"],
				Required:     true,
				Autocomplete: true,
			},
		},
		Handler:             HandlePlayNext,
		AutocompleteHandler: autocompleteVideoResults,
		TextOnly:            false,
		Usage:               cmd("playnext").Usage,
		Example:             cmd("playnext").Example,
	})
	command.RegisterAliases("playnext", cmd("playnext"))
	command.RegisterCommand(&command.Command{
		Name:        "search",
		Description: cmd("search").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "query",
				Description:  cmd("search").Options["query"],
				Required:     true,
				Autocomplete: true,
			},
		},
		Handler:             HandleSearch,
		AutocompleteHandler: autocompleteSuggestTerms,
		TextOnly:            false,
		Usage:               cmd("search").Usage,
		Example:             cmd("search").Example,
	})
	command.RegisterAliases("search", cmd("search"))
}
