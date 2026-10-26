package queue

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func HandleSwap(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, pos1, pos2, ok := readTwoPositions(s, i, messages.T(i.GuildID).Queue.SwapEnterPositions, messages.T(i.GuildID).Queue.EnterValidRange)
	if !ok {
		return nil
	}

	if (pos1 == 0 || pos2 == 0) && (q.Playing || q.Loading) {
		discord.RespondErrorEmbed(s, i, messages.T(i.GuildID).Queue.CannotSwapCurrent)
		return nil
	}

	if q.Songs[pos1].RequestedByID != i.Member.User.ID || q.Songs[pos2].RequestedByID != i.Member.User.ID {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission,
			messages.T(i.GuildID).Queue.OnlyOwnSwap))
		return nil
	}

	song1Title := q.Songs[pos1].Title
	song2Title := q.Songs[pos2].Title

	if err := queue.SwapSongs(i.GuildID, pos1, pos2); err != nil {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(messages.T(i.GuildID).Queue.SwapFailed, err))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T(i.GuildID).Queue.SwapCompleteTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Queue.SwapCompleteDesc,
			pos1+1, messages.EscapeMarkdown(song1Title),
			pos2+1, messages.EscapeMarkdown(song2Title)),
	}
	discord.RespondEmbed(s, i, embed)
	return nil
}
