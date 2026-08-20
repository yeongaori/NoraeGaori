package commands

import (
	"noraegaori/internal/commands/admin"
	"noraegaori/internal/commands/automix"
	"noraegaori/internal/commands/help"
	"noraegaori/internal/commands/play"
	"noraegaori/internal/commands/playback"
	"noraegaori/internal/commands/queue"
	"noraegaori/internal/commands/settings"
	"noraegaori/internal/commands/voice"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func InitializeCommands() {
	t := messages.T()
	cmd := func(name string) messages.CommandStrings {
		if t != nil {
			if c, ok := t.Commands[name]; ok {
				return c
			}
		}
		return messages.CommandStrings{}
	}

	play.Register(cmd)
	playback.Register(cmd)
	queue.Register(cmd)
	voice.Register(cmd)
	automix.Register(cmd)
	settings.Register(cmd)
	admin.Register(cmd)
	help.Register(cmd)

	logger.Debug("All commands registered")
}
