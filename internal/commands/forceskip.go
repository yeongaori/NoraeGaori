package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func HandleForceSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	songTitle := q.Songs[0].Title
	songURL := q.Songs[0].URL
	songThumbnail := q.Songs[0].Thumbnail

	err = player.Skip(s, i.GuildID)
	if err != nil && err != player.ErrQueueEmpty {
		logger.Errorf("[ForceSkip] Failed to skip: %v", err)
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err)))
		return nil
	}

	if err == player.ErrQueueEmpty {
		embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.PlaybackEndedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Music.ForceSkippedEnded, messages.FormatMaskedLink(songTitle, songURL)))
		messages.SetThumbnail(embed, songThumbnail)
		UpdateResponseEmbed(s, i, embed)

		if stopErr := player.Stop(i.GuildID); stopErr != nil {
			logger.Errorf("[ForceSkip] Failed to cleanup after queue empty: %v", stopErr)
		}
		return nil
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Skipped,
		fmt.Sprintf(messages.T(i.GuildID).Music.ForceSkipped, messages.FormatMaskedLink(songTitle, songURL)))
	messages.SetThumbnail(embed, songThumbnail)
	UpdateResponseEmbed(s, i, embed)
	return nil
}
