package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

// HandlePlayNext queues a song at index 1 so it plays immediately after the current one.
func HandlePlayNext(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterQuery))
		return nil
	}
	query := options[0].StringValue()
	query = messages.StripMarkdown(query)

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel))
		return nil
	}

	DeferResponse(s, i)

	searchEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Searching, fmt.Sprintf(messages.T(i.GuildID).Descriptions.Searching, query))
	UpdateResponseEmbed(s, i, searchEmbed)

	logger.Debugf("[PlayNext] Searching for: %s", query)
	song, err := youtube.Search(i.GuildID, query, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.SongNotFound))
		return err
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed))
			return err
		}
	} else {
		queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID)
	}

	queueSong := &queue.Song{
		URL:            song.URL,
		Title:          song.Title,
		Duration:       song.Duration,
		Thumbnail:      song.Thumbnail,
		Uploader:       song.Uploader,
		RequestedByID:  song.RequestedByID,
		RequestedByTag: song.RequestedBy,
		IsLive:         song.IsLive,
	}

	if err := queue.AddSong(i.GuildID, queueSong, 1); err != nil {
		if err.Error() == "song already in queue: "+song.Title {
			UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Duplicate, messages.T(i.GuildID).Errors.DuplicateSong))
		} else {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err)))
		}
		return err
	}

	q, _ = queue.GetQueue(i.GuildID, true)

	embed := messages.CreateSongEmbed(
		i.GuildID,
		messages.ColorSuccess,
		messages.T(i.GuildID).Music.AddedAsNext,
		"",
		song.Title, song.URL, song.Uploader,
		song.Duration, i.Member.User.Username,
		song.Thumbnail,
	)

	if len(q.Songs) == 1 {
		go player.Play(s, i.GuildID)
	}

	UpdateResponseEmbed(s, i, embed)
	return nil
}
