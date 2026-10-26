package voice

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func HandleJoin(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	voiceChannelID, ok := discord.CallerVoiceChannel(s, i)
	if !ok {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Voice.EnterVoiceChannel)
		return nil
	}

	discord.DeferResponse(s, i)

	if err := joinChannel(s, i.GuildID, voiceChannelID); err != nil {
		logger.Errorf("Failed to join voice channel for guild %s: %v", i.GuildID, err)
		_ = discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.JoinFailedTitle, messages.T(i.GuildID).Voice.JoinFailedDesc))
		return nil
	}

	voice := &messages.T(i.GuildID).Voice
	respondChannelSuccess(s, i, voiceChannelID, voice.JoinSuccessTitle, voice.JoinSuccessDesc, voice.JoinSuccessChannel)
	return nil
}

func respondChannelSuccess(s *discordgo.Session, i *discordgo.InteractionCreate, channelID, title, fallbackText, channelFormat string) {
	channel, err := s.Channel(channelID)
	if err != nil {
		_ = discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(title, fallbackText))
		return
	}

	_ = discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(title, fmt.Sprintf(channelFormat, messages.EscapeMarkdown(channel.Name))))
}

func joinChannel(s *discordgo.Session, guildID, channelID string) error {
	botChannelID := botVoiceChannelID(s, guildID)
	if botChannelID != "" && botChannelID != channelID && isPlaybackActive(guildID) {
		return moveToChannel(s, guildID, channelID)
	}
	return joinVoice(s, guildID, channelID)
}
