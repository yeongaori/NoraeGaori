package commands

import (
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func registerPlaybackCommands(cmd func(string) messages.CommandStrings) {
	RegisterCommand(&Command{
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
	registerCommandAliases("play", cmd("play"))

	RegisterCommand(&Command{
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
	registerCommandAliases("playnext", cmd("playnext"))

	RegisterCommand(&Command{
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
	registerCommandAliases("search", cmd("search"))

	RegisterCommand(&Command{
		Name:        "pause",
		Description: cmd("pause").Description,
		Handler:     HandlePause,
		TextOnly:    false,
		Usage:       cmd("pause").Usage,
		Example:     cmd("pause").Example,
	})
	registerCommandAliases("pause", cmd("pause"))

	RegisterCommand(&Command{
		Name:        "resume",
		Description: cmd("resume").Description,
		Handler:     HandleResume,
		TextOnly:    false,
		Usage:       cmd("resume").Usage,
		Example:     cmd("resume").Example,
	})
	registerCommandAliases("resume", cmd("resume"))

	RegisterCommand(&Command{
		Name:        "skip",
		Description: cmd("skip").Description,
		Handler:     HandleSkip,
		TextOnly:    false,
		Usage:       cmd("skip").Usage,
		Example:     cmd("skip").Example,
	})

	RegisterCommand(&Command{
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
	registerCommandAliases("seek", cmd("seek"))

	RegisterCommand(&Command{
		Name:        "stop",
		Description: cmd("stop").Description,
		Handler:     HandleStop,
		TextOnly:    false,
		Usage:       cmd("stop").Usage,
		Example:     cmd("stop").Example,
	})
	registerCommandAliases("stop", cmd("stop"))
}
