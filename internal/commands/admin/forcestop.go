package admin

import (
	"fmt"
	"noraegaori/internal/discord"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleForceStop(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Admin.NoSongsTitle, messages.T(i.GuildID).Admin.NoSongsDesc))
		return nil
	}

	if err := player.Stop(i.GuildID); err != nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Admin.StopFailed, err)))
		return err
	}

	discord.RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Admin.ForceStopTitle, messages.T(i.GuildID).Admin.ForceStopDesc))
	return nil
}
