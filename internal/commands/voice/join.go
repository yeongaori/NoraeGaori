package voice

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func HandleJoin(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Voice.EnterVoiceChannel))
		return nil
	}

	discord.DeferResponse(s, i)

	if err := joinChannel(s, i.GuildID, voiceState.ChannelID); err != nil {
		logger.Errorf("Failed to join voice channel for guild %s: %v", i.GuildID, err)
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.JoinFailedTitle, messages.T(i.GuildID).Voice.JoinFailedDesc))
		return nil
	}

	channel, err := s.Channel(voiceState.ChannelID)
	if err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.JoinSuccessTitle, messages.T(i.GuildID).Voice.JoinSuccessDesc))
		return nil
	}

	discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.JoinSuccessTitle, fmt.Sprintf(messages.T(i.GuildID).Voice.JoinSuccessChannel, messages.EscapeMarkdown(channel.Name))))
	return nil
}

func joinChannel(s *discordgo.Session, guildID, channelID string) error {
	botChannelID := botVoiceChannelID(s, guildID)
	if botChannelID != "" && botChannelID != channelID && isPlaybackActive(guildID) {
		return moveToChannel(s, guildID, channelID)
	}
	return joinVoice(s, guildID, channelID)
}
