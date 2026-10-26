package queue

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func HandleMoveTrack(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, fromPos, toPos, ok := readTwoPositions(s, i, messages.T(i.GuildID).Admin.EnterPositions, messages.T(i.GuildID).Admin.EnterValidRange)
	if !ok {
		return nil
	}

	if fromPos == toPos {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.SamePosition)
		return nil
	}

	if (fromPos == 0 || toPos == 0) && (q.Playing || q.Loading) {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Admin.CannotMovePlaying)
		return nil
	}

	songTitle := q.Songs[fromPos].Title

	if err := queue.MoveSong(i.GuildID, fromPos, toPos); err != nil {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Admin.MoveFailed, err))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(i.GuildID).Admin.MoveCompleteTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Admin.MoveCompleteDesc,
			messages.EscapeMarkdown(songTitle), fromPos+1, toPos+1),
	}
	discord.RespondEmbed(s, i, embed)
	return nil
}

func readTwoPositions(s *discordgo.Session, i *discordgo.InteractionCreate, missingText, rangeFormat string) (*queue.Queue, int, int, bool) {
	options := i.ApplicationCommandData().Options
	if len(options) < 2 {
		discord.RespondErrorEmbed(s, i, missingText)
		return nil, 0, 0, false
	}
	first := int(options[0].IntValue()) - 1
	second := int(options[1].IntValue()) - 1

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondEmbed(s, i, emptyQueueEmbed(i.GuildID))
		return nil, 0, 0, false
	}

	if first < 0 || second < 0 || first >= len(q.Songs) || second >= len(q.Songs) {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(rangeFormat, len(q.Songs)))
		return nil, 0, 0, false
	}
	return q, first, second, true
}
