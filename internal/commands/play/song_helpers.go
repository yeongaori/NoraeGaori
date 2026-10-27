package play

import (
	"errors"
	"fmt"

	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"

	"github.com/bwmarrin/discordgo"
)

func readPlayRequest(s *discordgo.Session, i *discordgo.InteractionCreate) (query, voiceChannelID string, ok bool) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Music.EnterQuery)
		return "", "", false
	}
	query = messages.StripMarkdown(options[0].StringValue())

	voiceChannelID, ok = discord.CallerVoiceChannel(s, i)
	if !ok {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Errors.NotInVoiceChannel)
		return "", "", false
	}

	discord.DeferResponse(s, i)

	searchEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Searching, fmt.Sprintf(messages.T(i.GuildID).Descriptions.Searching, query))
	_ = discord.UpdateResponseEmbed(s, i, searchEmbed)
	return query, voiceChannelID, true
}

func searchAndEnqueue(s *discordgo.Session, i *discordgo.InteractionCreate, query, voiceChannelID string, position int) (*youtube.Song, error) {
	logger.Debugf("Searching for: %s", query)
	song, err := youtube.Search(i.GuildID, query, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		if errors.Is(err, youtube.ErrUnsupportedYouTubeURL) {
			discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Music.UnsupportedURL)
			return nil, nil
		}
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Errors.SongNotFound)
		return nil, err
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return nil, err
	}

	if err := ensureQueueForVoice(s, i, q, voiceChannelID); err != nil {
		return nil, err
	}

	if err := queue.AddSong(i.GuildID, queueSongFrom(song), position); err != nil {
		if errors.Is(err, queue.ErrSongInQueue) {
			_ = discord.UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Duplicate, messages.T(i.GuildID).Errors.DuplicateSong))
		} else {
			discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err))
		}
		return nil, err
	}
	return song, nil
}

func youtubeSongEmbed(guildID string, color int, title, description string, song *youtube.Song) *discordgo.MessageEmbed {
	return messages.CreateSongEmbed(guildID, color, title, description, song.Title, song.URL, song.Uploader, song.Duration, song.RequestedBy, song.Thumbnail)
}

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

func addVideoFromPlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, song *youtube.Song) (*discordgo.MessageEmbed, bool, error) {
	isDuplicate := false
	if err := queue.AddSong(i.GuildID, queueSongFrom(song), -1); err != nil {
		if !errors.Is(err, queue.ErrSongInQueue) {
			discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err))
			return nil, false, err
		}
		isDuplicate = true
	}

	return videoWithPlaylistEmbed(i.GuildID, song, isDuplicate), isDuplicate, nil
}

func ensureQueueForVoice(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, voiceChannelID string) error {
	if q != nil {
		if err := queue.UpdateVoiceChannel(i.GuildID, voiceChannelID); err != nil {
			logger.Errorf("Failed to update voice channel for %s: %v", i.GuildID, err)
		}
		return nil
	}

	if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceChannelID); err != nil {
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Music.QueueCreateFailed)
		return err
	}
	return nil
}
