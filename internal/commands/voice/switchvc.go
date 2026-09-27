package voice

import (
	"errors"
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func HandleSwitchVC(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var targetChannelID string
	if len(options) > 0 {
		targetChannelID = options[0].ChannelValue(s).ID
	} else {
		voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
		if err != nil || voiceState.ChannelID == "" {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Voice.EnterVoiceOrSpecify))
			return nil
		}
		targetChannelID = voiceState.ChannelID
	}

	discord.DeferResponse(s, i)

	if err := moveToChannel(s, i.GuildID, targetChannelID); err != nil {
		reason := messages.T(i.GuildID).Voice.SwitchFailedChannel
		if errors.Is(err, errQueueUpdate) {
			reason = messages.T(i.GuildID).Voice.SwitchFailedQueue
		}
		logger.Errorf("Failed to switch voice channel for guild %s: %v", i.GuildID, err)
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.SwitchFailedTitle, reason))
		return nil
	}

	channel, err := s.Channel(targetChannelID)
	if err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.SwitchSuccessTitle, messages.T(i.GuildID).Voice.SwitchSuccessDesc))
		return nil
	}

	discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.SwitchSuccessTitle, fmt.Sprintf(messages.T(i.GuildID).Voice.SwitchSuccessChannel, messages.EscapeMarkdown(channel.Name))))
	return nil
}
