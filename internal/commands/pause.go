package commands

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandlePause(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel))
		return nil
	}

	
	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || (!q.Playing && !q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.NotPlayingOrLoading))
		return nil
	}

	if err := player.Pause(i.GuildID); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.PauseFailed))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Paused, messages.T(i.GuildID).Descriptions.Paused))
	return nil
}
