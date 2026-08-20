package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleStop(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterVoiceChannel))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	requiredVotes, err := requiredVotesInChannel(s, i.GuildID, voiceState.ChannelID, resolveWithFetch)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	if requiredVotes == 1 {
		return stopImmediately(s, i)
	}

	return startVote(s, i, voteRequest{
		kind:           voteKindStop,
		title:          messages.T(i.GuildID).Titles.StopVote,
		emoji:          "⏹",
		voiceChannelID: voiceState.ChannelID,
		requiredVotes:  requiredVotes,
		target:         voteTarget{scope: voteScopeWholeQueue},
		onPassed:       applyStopVote(i.GuildID),
	})
}

func stopImmediately(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	cancelSupersededVotes(i.GuildID, voteKindStop, false)

	if err := player.Stop(i.GuildID); err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, err)))
		return nil
	}

	UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc))
	return nil
}

func applyStopVote(guildID string) func(*discordgo.Session, *voteSession, voteTally) {
	return func(s *discordgo.Session, session *voteSession, tally voteTally) {
		cancelSupersededVotes(guildID, voteKindStop, false)

		if err := player.Stop(guildID); err != nil {
			renderVoteFailure(s, session, messages.T(guildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(guildID).Music.StopFailedDesc, err))
			return
		}

		renderVoteResult(s, session, messages.CreateSuccessEmbed(messages.T(guildID).Music.StopSuccessTitle, messages.T(guildID).Music.StopSuccessDesc), tally)
	}
}
