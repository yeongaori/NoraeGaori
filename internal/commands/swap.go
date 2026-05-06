package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func HandleSwap(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.SwapEnterPositions))
		return nil
	}
	pos1 := int(options[0].IntValue()) - 1
	pos2 := int(options[1].IntValue()) - 1

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.EmptyQueue, messages.T(i.GuildID).Descriptions.EmptyQueue))
		return nil
	}

	if pos1 < 0 || pos2 < 0 || pos1 >= len(q.Songs) || pos2 >= len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	if (pos1 == 0 || pos2 == 0) && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			messages.T(i.GuildID).Queue.CannotSwapCurrent))
		return nil
	}

	if q.Songs[pos1].RequestedByID != i.Member.User.ID || q.Songs[pos2].RequestedByID != i.Member.User.ID {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission,
			messages.T(i.GuildID).Queue.OnlyOwnSwap))
		return nil
	}

	song1Title := q.Songs[pos1].Title
	song2Title := q.Songs[pos2].Title

	if err := queue.SwapSongs(i.GuildID, pos1, pos2); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Queue.SwapFailed, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(i.GuildID).Queue.SwapCompleteTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Queue.SwapCompleteDesc,
			pos1+1, messages.EscapeMarkdown(song1Title),
			pos2+1, messages.EscapeMarkdown(song2Title)),
	}
	RespondEmbed(s, i, embed)
	return nil
}
