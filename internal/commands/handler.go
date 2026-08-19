package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"

	"github.com/bwmarrin/discordgo"
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
	commandsMu        sync.RWMutex
	commands          = make(map[string]*Command)
	aliases           = make(map[string]string)
	messageResponders sync.Map
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

func snapshotCommands() map[string]*Command {
	commandsMu.RLock()
	defer commandsMu.RUnlock()

	snapshot := make(map[string]*Command, len(commands))
	for name, cmd := range commands {
		snapshot[name] = cmd
	}
	return snapshot
}

func isGuildAdmin(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
	if member == nil {
		return false
	}

	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			logger.Debugf("Failed to get guild %s: %v", guildID, err)
			return false
		}
	}

	var perms int64 = 0

	for _, role := range guild.Roles {
		if role.ID == guildID {
			perms |= role.Permissions
			break
		}
	}

	for _, roleID := range member.Roles {
		for _, role := range guild.Roles {
			if role.ID == roleID {
				perms |= role.Permissions
				break
			}
		}
	}

	return (perms & discordgo.PermissionAdministrator) == discordgo.PermissionAdministrator
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

func registerCommandAliases(name string, cs messages.CommandStrings) {
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

	current := snapshotCommands()

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

	registerPlaybackCommands(cmd)
	registerQueueCommands(cmd)
	registerVoiceCommands(cmd)
	registerTogglesCommands(cmd)
	registerAutomixCommands(cmd)
	registerAdminCommands(cmd)
	registerHelpCommands(cmd)

	logger.Debug("All commands registered")
}

func RegisterSlashCommands(session *discordgo.Session) error {
	logger.Debug("Syncing slash commands with Discord...")

	appID := session.State.User.ID

	registered := snapshotCommands()

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
		}{cmd.Name, cmd.Description, opts}
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

func HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		HandleAutocomplete(s, i)
		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	cmdName := i.ApplicationCommandData().Name
	cmd, exists := lookupCommand(cmdName)
	if !exists {
		logger.Warnf("Unknown command: %s", cmdName)
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.UnknownCommand))
		return
	}

	if cmd.AdminOnly {
		if !config.IsAdmin(i.Member.User.ID) && !isGuildAdmin(s, i.GuildID, i.Member) {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission, messages.T(i.GuildID).Errors.AdminOnly))
			return
		}
	}

	logger.Debugf("Executing command: %s (user: %s, guild: %s)",
		cmdName, i.Member.User.Username, i.GuildID)

	if err := cmd.Handler(s, i); err != nil {
		logger.Errorf("Command %s failed: %v", cmdName, err)
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Errors.CommandExecutionError, err)))
	}
}

func HandleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {

	if m.Author.Bot {
		return
	}

	cfg := config.GetConfig()
	prefix := cfg.Prefix
	if m.GuildID != "" {
		if guildPrefix, err := queue.GetGuildPrefix(m.GuildID); err != nil {
			logger.Debugf("failed to get guild prefix for %s: %v", m.GuildID, err)
		} else if guildPrefix != "" {
			prefix = guildPrefix
		}
	}

	if !strings.HasPrefix(m.Content, prefix) {
		return
	}

	content := strings.TrimPrefix(m.Content, prefix)
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	_ = parts[1:]

	aliasTarget, ok := lookupAlias(cmdName)
	if !ok {
		return
	}
	cmdName = aliasTarget

	cmd, exists := lookupCommand(cmdName)
	if !exists {
		return
	}

	if cmd.AdminOnly {

		member, err := s.State.Member(m.GuildID, m.Author.ID)
		if err != nil {

			member, err = s.GuildMember(m.GuildID, m.Author.ID)
		}

		isBotAdmin := config.IsAdmin(m.Author.ID)
		isServerAdmin := (err == nil) && isGuildAdmin(s, m.GuildID, member)

		if !isBotAdmin && !isServerAdmin {
			embed := messages.CreateErrorEmbed(messages.T(m.GuildID).Titles.NoPermission, messages.T(m.GuildID).Errors.AdminOnly)

			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{embed},
				Reference: &discordgo.MessageReference{
					MessageID: m.ID,
					ChannelID: m.ChannelID,
				},
			})
			return
		}
	}

	logger.Debugf("Executing text command: %s (user: %s, guild: %s)",
		cmdName, m.Author.Username, m.GuildID)

	args := parts[1:]

	pseudoInteraction := CreatePseudoInteraction(s, m, cmd, args)

	messageResponder := &MessageResponse{
		Session:       s,
		ChannelID:     m.ChannelID,
		Message:       nil,
		OriginalMsgID: m.ID,
	}

	messageResponders.Store(pseudoInteraction.Token, messageResponder)
	defer messageResponders.Delete(pseudoInteraction.Token)

	if err := cmd.Handler(s, pseudoInteraction); err != nil {
		logger.Errorf("Text command %s failed: %v", cmdName, err)

		if messageResponder.Message == nil {
			embed := messages.CreateErrorEmbed(messages.T(m.GuildID).Titles.Error, fmt.Sprintf(messages.T(m.GuildID).Errors.CommandExecutionError, err))

			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{embed},
				Reference: &discordgo.MessageReference{
					MessageID: m.ID,
					ChannelID: m.ChannelID,
				},
			})
		}
	}
}

func isMessageCommand(i *discordgo.InteractionCreate) bool {
	return strings.HasPrefix(i.Token, "message_")
}

func RespondError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendMessage(message)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func RespondSuccess(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendMessage(message)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})
}

func RespondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendEmbed(embed)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

func RespondEmbedWithComponents(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			return mr.(*MessageResponse).SendEmbedWithComponents(embed, components)
		}
		return nil, fmt.Errorf("message responder not found")
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		return nil, err
	}

	return s.InteractionResponse(i.Interaction)
}

func DeferResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if isMessageCommand(i) {

		if mr, ok := messageResponders.Load(i.Token); ok {
			loadingEmbed := &discordgo.MessageEmbed{
				Color:       0xFFA500,
				Title:       messages.T(i.GuildID).Titles.Loading,
				Description: messages.T(i.GuildID).Descriptions.Loading,
			}
			mr.(*MessageResponse).SendEmbed(loadingEmbed)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
}

func FollowUpMessage(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendFollowUp(content)
		}
		return
	}
	s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Content: content,
	})
}

func FollowUpEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendFollowUpEmbed(embed)
		}
		return
	}
	s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

func UpdateResponseEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	if isMessageCommand(i) {

		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				_, err := s.ChannelMessageEditEmbed(responder.ChannelID, responder.Message.ID, embed)
				return err
			}
		}
		return fmt.Errorf("message responder or message not found")
	}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
	return err
}

func UpdateResponseEmbedWithComponents(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	if isMessageCommand(i) {

		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				logger.Debugf("Editing message %s in channel %s", responder.Message.ID, responder.ChannelID)
				_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:    responder.ChannelID,
					ID:         responder.Message.ID,
					Embeds:     &[]*discordgo.MessageEmbed{embed},
					Components: &components,
				})
				if err != nil {
					logger.Errorf("Failed to edit message: %v", err)
				}
				return err
			}
			logger.Errorf("Message is nil in responder")
			return fmt.Errorf("message is nil")
		}
		logger.Errorf("Message responder not found for token: %s", i.Token)
		return fmt.Errorf("message responder not found")
	}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
	return err
}

func GetResponseMessage(s *discordgo.Session, i *discordgo.InteractionCreate) (*discordgo.Message, error) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				return responder.Message, nil
			}
			return nil, fmt.Errorf("message not found in responder")
		}
		return nil, fmt.Errorf("message responder not found")
	}
	return s.InteractionResponse(i.Interaction)
}

func checkUserInBotVoiceChannel(s *discordgo.Session, i *discordgo.InteractionCreate) (string, *discordgo.MessageEmbed) {

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		return "", messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel)
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || q.VoiceChannelID == "" {

		return voiceState.ChannelID, nil
	}

	if voiceState.ChannelID != q.VoiceChannelID {
		return "", messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.MustBeInBotChannel)
	}

	return voiceState.ChannelID, nil
}

func buildLanguageChoices() []*discordgo.ApplicationCommandOptionChoice {
	codes := messages.AvailableLocales()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(codes))
	for _, code := range codes {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  code,
			Value: code,
		})
	}
	return choices
}
