package queue

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func HandleQueue(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
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

	panel := &queuePanel{
		guildID: i.GuildID,
		token:   discord.NewComponentToken(),
		perPage: songsPerPage,
		page:    currentPage,
	}

	embed := createQueueEmbed(i.GuildID, q.Songs, currentPage, totalPages, songsPerPage)
	components := createQueueButtons(i.GuildID, currentPage, totalPages, panel.token)

	removeHandler := s.AddHandler(panel.handleInteraction)

	panelMsg, err := discord.RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		removeHandler()
		logger.Errorf("Failed to send response: %v", err)
		return err
	}

	go expireQueuePanel(s, i, panelMsg, panel, removeHandler)

	return nil
}
