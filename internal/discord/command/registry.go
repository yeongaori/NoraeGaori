package command

import (
	"sync"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

type Command struct {
	Name                string
	Description         string
	Options             []*discordgo.ApplicationCommandOption
	Handler             func(s *discordgo.Session, i *discordgo.InteractionCreate) error
	AutocompleteHandler func(request AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice
	AdminOnly           bool
	TextOnly            bool
	Usage               string
	Example             string
}

var (
	commandsMu sync.RWMutex
	commands   = make(map[string]*Command)
	aliases    = make(map[string]string)
)

func lookupCommand(name string) (*Command, bool) {
	commandsMu.RLock()
	defer commandsMu.RUnlock()
	cmd, ok := commands[name]
	return cmd, ok
}

func lookupAlias(alias string) (string, bool) {
	commandsMu.RLock()
	defer commandsMu.RUnlock()
	name, ok := aliases[alias]
	return name, ok
}

func SnapshotAliases() map[string]string {
	commandsMu.RLock()
	defer commandsMu.RUnlock()

	snapshot := make(map[string]string, len(aliases))
	for alias, target := range aliases {
		snapshot[alias] = target
	}
	return snapshot
}

func Snapshot() map[string]*Command {
	commandsMu.RLock()
	defer commandsMu.RUnlock()

	snapshot := make(map[string]*Command, len(commands))
	for name, cmd := range commands {
		snapshot[name] = cmd
	}
	return snapshot
}

func RegisterCommand(cmd *Command) {
	commandsMu.Lock()
	commands[cmd.Name] = cmd
	commandsMu.Unlock()
	logger.Debugf("Registered command: %s", cmd.Name)
}

func RegisterAlias(alias, commandName string) {
	commandsMu.Lock()
	aliases[alias] = commandName
	commandsMu.Unlock()
	logger.Debugf("Registered alias: %s -> %s", alias, commandName)
}

func RegisterAliases(name string, cs messages.CommandStrings) {
	for _, alias := range cs.Aliases {
		RegisterAlias(alias, name)
	}
}

func ReloadAliases() {
	t := messages.T()
	cmd := func(name string) messages.CommandStrings {
		if t != nil {
			if c, ok := t.Commands[name]; ok {
				return c
			}
		}
		return messages.CommandStrings{}
	}

	current := Snapshot()

	rebuiltCommands := make(map[string]*Command, len(current))
	rebuiltAliases := make(map[string]string)

	for name, c := range current {
		cs := cmd(name)

		for _, alias := range cs.Aliases {
			rebuiltAliases[alias] = name
		}

		rebuilt := *c
		if cs.Description != "" {
			rebuilt.Description = cs.Description
		}
		if cs.Usage != "" {
			rebuilt.Usage = cs.Usage
		}
		if cs.Example != "" {
			rebuilt.Example = cs.Example
		}
		rebuiltCommands[name] = &rebuilt
	}

	commandsMu.Lock()
	commands = rebuiltCommands
	aliases = rebuiltAliases
	commandsMu.Unlock()

	logger.Info("Aliases and descriptions reloaded for new locale")
}
