package commands

import (
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"

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
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed))
		return err
	}
	return nil
}

func startPlaybackIfFirstSong(s *discordgo.Session, guildID string) {
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 1 {
		return
	}

	p := player.GetPlayer(guildID)
	if !p.Playing && !p.Loading {
		go player.Play(s, guildID)
	}
}

func resumeOrStartPlayback(s *discordgo.Session, guildID string) {
	p := player.GetPlayer(guildID)

	switch {
	case p.Paused:
		logger.Debugf("Resuming playback for guild %s", guildID)
		go player.Resume(s, guildID)
	case !p.Playing && !p.Loading:
		logger.Debugf("Starting playback for guild %s", guildID)
		go player.Play(s, guildID)
	}
}
