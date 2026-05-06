package commands

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

// HandleQueue renders the current queue, paginated 10 songs per page.
func HandleQueue(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
		return nil
	}

	const songsPerPage = 10
	totalSongs := len(q.Songs)
	totalPages := (totalSongs + songsPerPage - 1) / songsPerPage
	currentPage := 1

	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		currentPage = int(options[0].IntValue())
		if currentPage < 1 {
			currentPage = 1
		}
		if currentPage > totalPages {
			currentPage = totalPages
		}
	}

	embed := createQueueEmbed(i.GuildID, q.Songs, currentPage, totalPages, songsPerPage)

	if totalPages == 1 {
		RespondEmbed(s, i, embed)
		return nil
	}

	components := createQueueButtons(i.GuildID, currentPage, totalPages)

	msg, err := RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		logger.Errorf("[Queue] Failed to send response: %v", err)
		return err
	}

	go handleQueueButtons(s, i, msg, i.GuildID, totalPages, songsPerPage)

	return nil
}
