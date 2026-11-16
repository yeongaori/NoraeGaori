package voice

import (
	"errors"
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
		if !canConnect(s, i.Member.User.ID, targetChannelID) {
			discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Voice.ConnectDenied)
			return nil
		}
	} else {
		voiceChannelID, ok := discord.CallerVoiceChannel(s, i)
		if !ok {
			discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Voice.EnterVoiceOrSpecify)
			return nil
		}
		targetChannelID = voiceChannelID
	}

	if !guardVoiceAccess(s, i, moveAction, targetChannelID, botVoiceChannelID(s, i.GuildID)) {
		return nil
	}

	discord.DeferResponse(s, i)
	switchChannel(s, i, targetChannelID)
	return nil
}

func switchChannel(s *discordgo.Session, i *discordgo.InteractionCreate, targetChannelID string) {
	if err := moveToChannel(s, i.GuildID, targetChannelID); err != nil {
		reason := messages.T(i.GuildID).Voice.SwitchFailedChannel
		if errors.Is(err, errQueueUpdate) {
			reason = messages.T(i.GuildID).Voice.SwitchFailedQueue
		}
		logger.Errorf("Failed to switch voice channel for guild %s: %v", i.GuildID, err)
		_ = discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.SwitchFailedTitle, reason))
		return
	}

	voice := &messages.T(i.GuildID).Voice
	respondChannel(s, i, targetChannelID, voice.SwitchSuccessTitle, voice.SwitchSuccessDesc, voice.SwitchSuccessChannel)
}
