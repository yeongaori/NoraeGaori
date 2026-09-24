//go:build faults

package faultcheck

import (
	"fmt"
	"slices"
	"sort"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord/command"
)

var audioCommands = map[string]struct{}{
	"play":      {},
	"playnext":  {},
	"search":    {},
	"pause":     {},
	"resume":    {},
	"skip":      {},
	"seek":      {},
	"stop":      {},
	"join":      {},
	"leave":     {},
	"switchvc":  {},
	"forceskip": {},
	"forcestop": {},
}

var extraArguments = map[string][][]string{
	"remove":      {{"all"}, {"1-2"}},
	"forceremove": {{"1"}},
}

func Commands() []*command.Command {
	registered := command.Snapshot()
	commands := make([]*command.Command, 0, len(registered))
	for name, cmd := range registered {
		if _, isAudio := audioCommands[name]; !isAudio {
			commands = append(commands, cmd)
		}
	}
	sort.Slice(commands, func(first, second int) bool { return commands[first].Name < commands[second].Name })
	return commands
}

func Arguments(cmd *command.Command) [][]string {
	samples := make([][]string, len(cmd.Options))
	base := make([]string, len(cmd.Options))
	variantCount := 0
	for index, option := range cmd.Options {
		samples[index] = sampleValues(option)
		base[index] = samples[index][0]
		variantCount += len(samples[index]) - 1
	}

	sets := make([][]string, 0, 2+variantCount+len(extraArguments[cmd.Name]))
	sets = append(sets, nil)
	if len(base) > 0 {
		sets = append(sets, base)
	}
	for index := range samples {
		for _, value := range samples[index][1:] {
			variant := slices.Clone(base)
			variant[index] = value
			sets = append(sets, variant)
		}
	}
	return append(sets, extraArguments[cmd.Name]...)
}

func sampleValues(option *discordgo.ApplicationCommandOption) []string {
	if len(option.Choices) > 0 {
		values := make([]string, 0, len(option.Choices))
		for _, choice := range option.Choices {
			values = append(values, fmt.Sprint(choice.Value))
		}
		return values
	}
	if option.Type == discordgo.ApplicationCommandOptionInteger || option.Type == discordgo.ApplicationCommandOptionNumber {
		return []string{"1", "2"}
	}
	return []string{"1"}
}
