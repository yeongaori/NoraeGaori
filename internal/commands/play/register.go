package play

import (
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"

	"github.com/bwmarrin/discordgo"
)

func Register(cmd func(string) messages.CommandStrings) {
	command.RegisterLocalized(cmd, &command.Command{
		Name:                "play",
		Options:             []*discordgo.ApplicationCommandOption{command.QueryOption(cmd("play").Options["query"])},
		Handler:             HandlePlay,
		AutocompleteHandler: autocompleteVideoResults,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:                "playnext",
		Options:             []*discordgo.ApplicationCommandOption{command.QueryOption(cmd("playnext").Options["query"])},
		Handler:             HandlePlayNext,
		AutocompleteHandler: autocompleteVideoResults,
	})
	command.RegisterLocalized(cmd, &command.Command{
		Name:                "search",
		Options:             []*discordgo.ApplicationCommandOption{command.QueryOption(cmd("search").Options["query"])},
		Handler:             HandleSearch,
		AutocompleteHandler: autocompleteSuggestTerms,
	})
}
