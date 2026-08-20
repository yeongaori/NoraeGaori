package play

import (
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"

	"github.com/bwmarrin/discordgo"
)

func queueSongFrom(song *youtube.Song) *queue.Song {
	return &queue.Song{
		URL:            song.URL,
		Title:          song.Title,
		Duration:       song.Duration,
		Thumbnail:      song.Thumbnail,
		Uploader:       song.Uploader,
		RequestedByID:  song.RequestedByID,
		RequestedByTag: song.RequestedBy,
		IsLive:         song.IsLive,
	}
}

func ensureQueueForVoice(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, voiceChannelID string) error {
	if q != nil {
		if err := queue.UpdateVoiceChannel(i.GuildID, voiceChannelID); err != nil {
			logger.Errorf("Failed to update voice channel for %s: %v", i.GuildID, err)
		}
		return nil
	}

	if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceChannelID); err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed))
		return err
	}
	return nil
}
