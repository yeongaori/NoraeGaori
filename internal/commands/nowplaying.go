package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleNowPlaying(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	song := q.Songs[0]

	var title string
	var color int
	if q.Loading {
		title = messages.T(i.GuildID).Music.NowPlayingLoading
		color = messages.ColorWarning
	} else if q.Playing {
		title = messages.T(i.GuildID).Music.NowPlayingPlaying
		color = messages.ColorSuccess
	} else {
		title = messages.T(i.GuildID).Music.NowPlayingPaused
		color = messages.ColorPaused
	}

	progressText := song.Duration
	if q.Playing && !song.IsLive {
		position := player.GetCurrentPosition(i.GuildID)
		positionStr := player.FormatDuration(position)
		progressText = fmt.Sprintf("%s / %s", positionStr, song.Duration)
	}

	embed := &discordgo.MessageEmbed{
		Color:       color,
		Title:       title,
		Description: messages.FormatBoldMaskedLink(song.Title, song.URL),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Fields.Uploader, Value: messages.EscapeMarkdown(song.Uploader), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Duration, Value: progressText, Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: messages.EscapeMarkdown(song.RequestedByTag), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
	}

	if len(q.Songs) > 1 {
		messages.AddField(embed, messages.T(i.GuildID).Fields.NextSong, fmt.Sprintf("**%s**", messages.EscapeMarkdown(q.Songs[1].Title)), false)
	}

	RespondEmbed(s, i, embed)
	return nil
}
