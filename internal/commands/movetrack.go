package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func HandleMoveTrack(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.EnterPositions))
		return nil
	}
	fromPos := int(options[0].IntValue()) - 1
	toPos := int(options[1].IntValue()) - 1

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
		return nil
	}

	if fromPos < 0 || toPos < 0 || fromPos >= len(q.Songs) || toPos >= len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Admin.EnterValidRange, len(q.Songs))))
		return nil
	}

	if fromPos == toPos {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Admin.SamePosition))
		return nil
	}

	
	if (fromPos == 0 || toPos == 0) && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Admin.CannotMovePlaying))
		return nil
	}

	songTitle := q.Songs[fromPos].Title

	if err := queue.MoveSong(i.GuildID, fromPos, toPos); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Admin.MoveFailed, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(i.GuildID).Admin.MoveCompleteTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Admin.MoveCompleteDesc,
			messages.EscapeMarkdown(songTitle), fromPos+1, toPos+1),
	}
	RespondEmbed(s, i, embed)
	return nil
}
