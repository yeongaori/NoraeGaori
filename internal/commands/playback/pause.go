package playback

import (
	"errors"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandlePause(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if _, ok := discord.CallerVoiceChannel(s, i); !ok {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Errors.NotInVoiceChannel)
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || (!q.Playing && !q.Loading) {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.NotPlayingOrLoading)
		return nil
	}

	if err := player.Pause(i.GuildID); errors.Is(err, player.ErrNotPlaying) {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.NotPlayingOrLoading)
		return nil
	} else if err != nil {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.PauseFailed)
		return err
	}

	discord.RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Paused, pausedDescription(i.GuildID)))
	return nil
}

func pausedDescription(guildID string) string {
	if player.ShouldLeaveVoice(guildID) {
		return messages.T(guildID).Descriptions.Paused
	}
	return messages.T(guildID).Descriptions.PausedStay
}
