package command

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/discord"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

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
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.UnknownCommand))
		return
	}

	member := discord.InteractionMember(i)
	if member == nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.GuildOnly))
		return
	}

	if cmd.AdminOnly && !discord.IsAdminMember(s, i.GuildID, member) {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission, messages.T(i.GuildID).Errors.AdminOnly))
		return
	}

	logger.Debugf("Executing command: %s (user: %s, guild: %s)",
		cmdName, member.User.Username, i.GuildID)

	if err := cmd.Handler(s, i); err != nil {
		logger.Errorf("Command %s failed: %v", cmdName, err)
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Errors.CommandExecutionError, err)))
	}
}

func HandleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {

	if m.Author.Bot {
		return
	}

	cfg := config.GetConfig()
	prefix := cfg.Prefix
	if m.GuildID != "" {
		if guildPrefix, err := guild.GetPrefix(m.GuildID); err != nil {
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
		isServerAdmin := (err == nil) && discord.IsGuildAdmin(s, m.GuildID, member)

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

	pseudoInteraction := discord.CreatePseudoInteraction(s, m, cmd.Name, cmd.Options, args)

	messageResponder := &discord.MessageResponse{
		Session:       s,
		ChannelID:     m.ChannelID,
		Message:       nil,
		OriginalMsgID: m.ID,
	}

	defer discord.RegisterResponder(pseudoInteraction.Token, messageResponder)()

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
