package discord

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func IsGuildAdmin(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
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

func CheckUserInBotVoiceChannel(s *discordgo.Session, i *discordgo.InteractionCreate) (string, *discordgo.MessageEmbed) {

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
