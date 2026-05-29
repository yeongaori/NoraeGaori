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

func HandlePlay(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	
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

	if youtube.IsYouTubeURL(query) {
		analysis := youtube.AnalyzeYouTubeURL(query)
		logger.Debugf("[Play] URL analysis: type=%s, videoID=%s, playlistID=%s", analysis.Type, analysis.VideoID, analysis.PlaylistID)

		if analysis.Type == youtube.URLTypePurePlaylist {
			return handlePurePlaylist(s, i, query, voiceState)
		}

		if analysis.Type == youtube.URLTypeVideoWithPlaylist {
			return handleVideoWithPlaylist(s, i, query, analysis, voiceState)
		}
	}

	logger.Debugf("[Play] Searching for: %s", query)
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

	if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
		if err.Error() == "song already in queue: "+song.Title {
			UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.Duplicate, messages.T(i.GuildID).Errors.DuplicateSong))
		} else {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err)))
		}
		return err
	}

	q, _ = queue.GetQueue(i.GuildID, true)

	isFirstSong := len(q.Songs) == 1

	var embed *discordgo.MessageEmbed
	if isFirstSong {
		uploaderValue := song.Uploader
		if uploaderValue == "" {
			uploaderValue = "-"
		}
		durationValue := song.Duration
		if durationValue == "" {
			durationValue = "-"
		}
		embed = &discordgo.MessageEmbed{
			Color:       messages.ColorWarning,
			Title:       messages.T(i.GuildID).Titles.Loading,
			Description: fmt.Sprintf("%s\n\n%s", messages.FormatBoldMaskedLink(song.Title, song.URL), messages.T(i.GuildID).Descriptions.Loading),
			Fields: []*discordgo.MessageEmbedField{
				{Name: messages.T(i.GuildID).Fields.Uploader, Value: uploaderValue, Inline: true},
				{Name: messages.T(i.GuildID).Fields.Duration, Value: durationValue, Inline: true},
				{Name: messages.T(i.GuildID).Fields.Requester, Value: i.Member.User.Username, Inline: true},
			},
		}
		if song.Thumbnail != "" {
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail}
		}
	} else {
		embed = messages.CreateSongEmbed(
			i.GuildID,
			messages.ColorSuccess,
			messages.T(i.GuildID).Titles.Added,
			"",
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedBy,
			song.Thumbnail,
		)
	}

	UpdateResponseEmbed(s, i, embed)

	
	if isFirstSong {
		msg, err := GetResponseMessage(s, i)
		if err == nil {
			player.SetLoadingMessage(i.GuildID, msg)
		}
	}

	
	p := player.GetPlayer(i.GuildID)
	switch {
	case p.Paused:
		go player.Resume(s, i.GuildID)
	case !p.Playing && !p.Loading:
		go player.Play(s, i.GuildID)
	}

	return nil
}
