package voice

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/vote"
)

var cancelSkipVotes = vote.CancelSkipVotes

func HandleLeave(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if botVoiceChannelID(s, i.GuildID) == "" {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Voice.BotNotInVoice))
		return nil
	}

	discord.DeferResponse(s, i)
	cancelSkipVotes(i.GuildID)

	if err := suspendPlayback(i.GuildID); err != nil {
		logger.Errorf("Failed to leave voice for guild %s: %v", i.GuildID, err)
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Voice.LeaveFailedTitle, messages.T(i.GuildID).Voice.LeaveFailedDesc))
		return nil
	}

	description := messages.T(i.GuildID).Voice.LeaveSuccessDesc
	if hasQueuedSongs(i.GuildID) {
		description = messages.T(i.GuildID).Voice.LeavePausedDesc
	}
	discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Voice.LeaveSuccessTitle, description))
	return nil
}

func hasQueuedSongs(guildID string) bool {
	q, err := queue.GetQueue(guildID, false)
	return err == nil && q != nil && len(q.Songs) > 0
}
