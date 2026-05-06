package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
)

func HandleJoin(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Voice.EnterVoiceChannel))
		return nil
	}

	_, err = player.JoinVoice(s, i.GuildID, voiceState.ChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.JoinFailedTitle, messages.T(i.GuildID).Voice.JoinFailedDesc))
		return err
	}

	channel, err := s.Channel(voiceState.ChannelID)
	if err != nil {
		RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.JoinSuccessTitle, messages.T(i.GuildID).Voice.JoinSuccessDesc))
		return nil
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.JoinSuccessTitle, fmt.Sprintf(messages.T(i.GuildID).Voice.JoinSuccessChannel, messages.EscapeMarkdown(channel.Name))))
	return nil
}
