package admin

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleForceSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	discord.DeferResponse(s, i)

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	songTitle := q.Songs[0].Title
	songURL := q.Songs[0].URL
	songThumbnail := q.Songs[0].Thumbnail

	err = player.Skip(s, i.GuildID)
	if err != nil && err != player.ErrQueueEmpty {
		logger.Errorf("Failed to skip: %v", err)
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err)))
		return nil
	}

	if err == player.ErrQueueEmpty {
		embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.PlaybackEndedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Music.ForceSkippedEnded, messages.FormatMaskedLink(songTitle, songURL)))
		messages.SetThumbnail(embed, songThumbnail)
		discord.UpdateResponseEmbed(s, i, embed)

		if stopErr := player.Stop(i.GuildID); stopErr != nil {
			logger.Errorf("Failed to cleanup after queue empty: %v", stopErr)
		}
		return nil
	}

	embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.Skipped,
		fmt.Sprintf(messages.T(i.GuildID).Music.ForceSkipped, messages.FormatMaskedLink(songTitle, songURL)))
	messages.SetThumbnail(embed, songThumbnail)
	discord.UpdateResponseEmbed(s, i, embed)
	return nil
}
