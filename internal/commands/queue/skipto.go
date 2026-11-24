package queue

import (
	"errors"
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleSkipTo(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	discord.DeferResponse(s, i)

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Queue.SkipToEnterPosition)
		return nil
	}
	position := int(options[0].IntValue())

	if _, ok := discord.CallerVoiceChannel(s, i); !ok {
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Music.EnterVoiceChannel)
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		_ = discord.UpdateResponseEmbed(s, i, emptyQueueEmbed(i.GuildID))
		return nil
	}

	if position < 1 || position > len(q.Songs) {
		discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Queue.EnterValidRange, len(q.Songs)))
		return nil
	}

	if position == 1 {
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Queue.SkipToCurrent)
		return nil
	}

	targetSong := q.Songs[position-1]

	if err := queue.SkipToSong(i.GuildID, targetSong.ID); errors.Is(err, queue.ErrSongIsCurrent) {
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Queue.SkipToCurrent)
		return nil
	} else if err != nil {
		discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToFailed, err))
		return err
	}

	if player.IsPlaybackActive(i.GuildID) {
		if err := player.SkipTo(s, i.GuildID); err != nil {
			discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToFailed, err))
			return err
		}
	}

	_ = discord.UpdateResponseEmbed(s, i, skipToEmbed(i.GuildID, targetSong, position-1))
	return nil
}

func skipToEmbed(guildID string, song *queue.Song, skipped int) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(guildID).Queue.SkipToCompleteTitle,
		Description: fmt.Sprintf(messages.T(guildID).Queue.SkipToCompleteDesc,
			messages.FormatBoldMaskedLink(song.Title, song.URL)),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(guildID).Queue.SkipToSkippedSongs, Value: fmt.Sprintf(messages.T(guildID).Queue.SkipToSongsCount, skipped), Inline: true},
			{Name: messages.T(guildID).Fields.Requester, Value: messages.EscapeMarkdown(song.RequestedByTag), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
	}
}
