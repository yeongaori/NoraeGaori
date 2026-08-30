package command

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
)

func RegisterSlashCommands(session *discordgo.Session) error {
	logger.Debug("Syncing slash commands with Discord...")

	appID := session.State.User.ID

	registered := Snapshot()

	guildContexts := []discordgo.InteractionContextType{discordgo.InteractionContextGuild}

	desired := make([]*discordgo.ApplicationCommand, 0, len(registered))
	for _, cmd := range registered {
		if cmd.TextOnly {
			logger.Debugf("Skipping text-only command from slash registration: %s", cmd.Name)
			continue
		}
		desired = append(desired, &discordgo.ApplicationCommand{
			Name:        cmd.Name,
			Description: cmd.Description,
			Options:     cmd.Options,
			Contexts:    &guildContexts,
		})
	}

	fillMissingCommandDescriptions(desired)

	existing, err := session.ApplicationCommands(appID, "")
	if err != nil {
		return fmt.Errorf("failed to get existing commands: %w", err)
	}

	desiredJSON, err := canonicalCommandMap(desired)
	if err != nil {
		return fmt.Errorf("failed to canonicalize desired commands: %w", err)
	}
	existingJSON, err := canonicalCommandMap(existing)
	if err != nil {
		return fmt.Errorf("failed to canonicalize existing commands: %w", err)
	}

	added, updated, removed := diffCommandSets(desiredJSON, existingJSON)
	if len(added) == 0 && len(updated) == 0 && len(removed) == 0 {
		logger.Debug("Slash commands already in sync, skipping registration")
		return nil
	}

	logger.Infof("Slash command changes detected — added: %v, updated: %v, removed: %v", added, updated, removed)

	if _, err := session.ApplicationCommandBulkOverwrite(appID, "", desired); err != nil {
		return fmt.Errorf("failed to bulk overwrite slash commands: %w", err)
	}

	logger.Info("Slash commands registered successfully")
	return nil
}

func fillMissingCommandDescriptions(cmds []*discordgo.ApplicationCommand) {
	for _, command := range cmds {
		if command.Description == "" {
			logger.Errorf("Missing locale description for command %q, using its name as a placeholder", command.Name)
			command.Description = command.Name
		}
		for _, option := range command.Options {
			if option.Description == "" {
				logger.Errorf("Missing locale description for option %q of command %q, using its name as a placeholder", option.Name, command.Name)
				option.Description = option.Name
			}
		}
	}
}

func canonicalContexts(contexts *[]discordgo.InteractionContextType) []discordgo.InteractionContextType {
	if contexts == nil || len(*contexts) == 0 {
		return []discordgo.InteractionContextType{
			discordgo.InteractionContextGuild,
			discordgo.InteractionContextBotDM,
			discordgo.InteractionContextPrivateChannel,
		}
	}

	sorted := append([]discordgo.InteractionContextType(nil), *contexts...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	return sorted
}

func canonicalCommandMap(cmds []*discordgo.ApplicationCommand) (map[string]string, error) {
	out := make(map[string]string, len(cmds))
	for _, cmd := range cmds {
		opts := cmd.Options
		if opts == nil {
			opts = []*discordgo.ApplicationCommandOption{}
		}
		shape := struct {
			Name        string                                `json:"name"`
			Description string                                `json:"description"`
			Options     []*discordgo.ApplicationCommandOption `json:"options"`
			Contexts    []discordgo.InteractionContextType    `json:"contexts"`
		}{cmd.Name, cmd.Description, opts, canonicalContexts(cmd.Contexts)}
		buf, err := json.Marshal(shape)
		if err != nil {
			return nil, fmt.Errorf("marshal command %q: %w", cmd.Name, err)
		}
		out[cmd.Name] = string(buf)
	}
	return out, nil
}

func diffCommandSets(desired, existing map[string]string) (added, updated, removed []string) {
	for name, want := range desired {
		got, ok := existing[name]
		if !ok {
			added = append(added, name)
		} else if got != want {
			updated = append(updated, name)
		}
	}
	for name := range existing {
		if _, ok := desired[name]; !ok {
			removed = append(removed, name)
		}
	}
	return added, updated, removed
}
