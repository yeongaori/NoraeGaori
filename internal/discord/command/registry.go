package command

import (
	"sync"
	"sync/atomic"

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
	registryWrite sync.Mutex
	commands      atomic.Pointer[map[string]*Command]
	aliases       atomic.Pointer[map[string]string]
)

func init() {
	commands.Store(&map[string]*Command{})
	aliases.Store(&map[string]string{})
}

func lookupCommand(name string) (*Command, bool) {
	cmd, ok := (*commands.Load())[name]
	return cmd, ok
}

func lookupAlias(alias string) (string, bool) {
	name, ok := (*aliases.Load())[alias]
	return name, ok
}

func SnapshotAliases() map[string]string {
	return *aliases.Load()
}

func Snapshot() map[string]*Command {
	return *commands.Load()
}

func RegisterCommand(cmd *Command) {
	registryWrite.Lock()

	rebuilt := make(map[string]*Command, len(*commands.Load())+1)
	for name, existing := range *commands.Load() {
		rebuilt[name] = existing
	}
	rebuilt[cmd.Name] = cmd
	commands.Store(&rebuilt)

	registryWrite.Unlock()
	logger.Debugf("Registered command: %s", cmd.Name)
}

func RegisterAlias(alias, commandName string) {
	registryWrite.Lock()

	rebuilt := make(map[string]string, len(*aliases.Load())+1)
	for existing, target := range *aliases.Load() {
		rebuilt[existing] = target
	}
	rebuilt[alias] = commandName
	aliases.Store(&rebuilt)

	registryWrite.Unlock()
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

	registryWrite.Lock()
	commands.Store(&rebuiltCommands)
	aliases.Store(&rebuiltAliases)
	registryWrite.Unlock()

	logger.Info("Aliases and descriptions reloaded for new locale")
}
