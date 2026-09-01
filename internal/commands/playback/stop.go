package playback

import (
	"fmt"
	"noraegaori/internal/discord"
	"noraegaori/internal/vote"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleStop(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	discord.DeferResponse(s, i)

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.EnterVoiceChannel))
		return nil
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoSong, messages.T(i.GuildID).Errors.EmptyQueue))
		return nil
	}

	requiredVotes, err := vote.RequiredInChannel(s, i.GuildID, voiceState.ChannelID, vote.ResolveWithFetch)
	if err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	if requiredVotes == 1 {
		return stopImmediately(s, i)
	}

	return vote.Start(s, i, vote.Request{
		Kind:           vote.KindStop,
		Title:          messages.T(i.GuildID).Titles.StopVote,
		Emoji:          "⏹",
		VoiceChannelID: voiceState.ChannelID,
		RequiredVotes:  requiredVotes,
		Target:         vote.Target{Scope: vote.ScopeWholeQueue},
		OnPassed:       applyStopVote(i.GuildID),
	})
}

func stopImmediately(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	vote.CancelSuperseded(i.GuildID, vote.KindStop, false)

	if err := player.Stop(i.GuildID); err != nil {
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, err)))
		return nil
	}

	discord.UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc))
	return nil
}

func applyStopVote(guildID string) func(*discordgo.Session, *vote.Session, vote.Tally) {
	return func(s *discordgo.Session, session *vote.Session, tally vote.Tally) {
		vote.CancelSuperseded(guildID, vote.KindStop, false)

		if err := player.Stop(guildID); err != nil {
			vote.RenderFailure(s, session, messages.T(guildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(guildID).Music.StopFailedDesc, err))
			return
		}

		vote.RenderResult(s, session, messages.CreateSuccessEmbed(messages.T(guildID).Music.StopSuccessTitle, messages.T(guildID).Music.StopSuccessDesc), tally)
	}
}
