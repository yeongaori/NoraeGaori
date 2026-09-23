package queue

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
)

func HandleQueue(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondEmbed(s, i, emptyQueueEmbed(i.GuildID))
		return nil
	}

	page := 1
	if options := i.ApplicationCommandData().Options; len(options) > 0 {
		page = int(options[0].IntValue())
	}

	embed, components := renderQueuePage(i.GuildID, q.Songs, page)
	if _, err := discord.SendEmbedWithComponents(s, i, embed, components); err != nil {
		logger.Errorf("Failed to send response: %v", err)
		return err
	}
	return nil
}
