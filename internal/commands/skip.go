package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func skipResultEmbed(guildID string, song *queue.Song, queueEnded bool) *discordgo.MessageEmbed {
	title := messages.T(guildID).Titles.Skipped
	template := messages.T(guildID).Descriptions.Skipped
	if queueEnded {
		title = messages.T(guildID).Music.PlaybackEndedTitle
		template = messages.T(guildID).Music.PlaybackEndedSkip
	}

	embed := messages.CreateSuccessEmbed(title, fmt.Sprintf(template, messages.FormatMaskedLink(song.Title, song.URL)))
	messages.SetThumbnail(embed, song.Thumbnail)
	return embed
}

func stopAfterEmptyQueue(guildID string) {
	if err := player.Stop(guildID); err != nil {
		logger.Errorf("Failed to cleanup after queue empty: %v", err)
	}
}

func applySkipVote(guildID string, skipped *queue.Song) func(*discordgo.Session, *voteSession, voteTally) {
	return func(s *discordgo.Session, session *voteSession, tally voteTally) {
		skipErr := player.Skip(s, guildID)
		if skipErr != nil && skipErr != player.ErrQueueEmpty {
			renderVoteFailure(s, session, messages.T(guildID).Music.SkipFailedTitle, fmt.Sprintf(messages.T(guildID).Music.SkipFailedDesc, skipErr))
			return
		}

		queueEnded := skipErr == player.ErrQueueEmpty
		cancelSupersededVotes(guildID, voteKindSkip, queueEnded)

		renderVoteResult(s, session, skipResultEmbed(guildID, skipped, queueEnded), tally)

		if queueEnded {
			stopAfterEmptyQueue(guildID)
		}
	}
}

func HandleSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
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

	skipped := q.Songs[0]

	if requiredVotes == 1 {
		return skipImmediately(s, i, skipped)
	}

	return startVote(s, i, voteRequest{
		kind:           voteKindSkip,
		title:          messages.T(i.GuildID).Titles.SkipVote,
		emoji:          "⏭",
		voiceChannelID: voiceState.ChannelID,
		requiredVotes:  requiredVotes,
		target:         voteTarget{scope: voteScopeCurrentSong},
		onPassed:       applySkipVote(i.GuildID, skipped),
	})
}

func skipImmediately(s *discordgo.Session, i *discordgo.InteractionCreate, skipped *queue.Song) error {
	err := player.Skip(s, i.GuildID)
	if err != nil && err != player.ErrQueueEmpty {
		logger.Errorf("Failed to skip: %v", err)
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle,
			fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err)))
		return nil
	}

	queueEnded := err == player.ErrQueueEmpty
	cancelSupersededVotes(i.GuildID, voteKindSkip, queueEnded)

	UpdateResponseEmbed(s, i, skipResultEmbed(i.GuildID, skipped, queueEnded))

	if queueEnded {
		stopAfterEmptyQueue(i.GuildID)
	}
	return nil
}
