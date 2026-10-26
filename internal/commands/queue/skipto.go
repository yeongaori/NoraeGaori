package queue

import (
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

	if err := queue.SkipToPosition(i.GuildID, position-1); err != nil {
		discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToFailed, err))
		return err
	}

	if player.IsPlaybackActive(i.GuildID) {
		if err := player.SkipTo(s, i.GuildID); err != nil {
			discord.UpdateErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToFailed, err))
			return err
		}
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(i.GuildID).Queue.SkipToCompleteTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToCompleteDesc,
			messages.FormatBoldMaskedLink(targetSong.Title, targetSong.URL)),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Queue.SkipToSkippedSongs, Value: fmt.Sprintf(messages.T(i.GuildID).Queue.SkipToSongsCount, position-1), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: messages.EscapeMarkdown(targetSong.RequestedByTag), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: targetSong.Thumbnail},
	}

	_ = discord.UpdateResponseEmbed(s, i, embed)
	return nil
}
