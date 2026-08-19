package commands

import (
	"fmt"
	"time"

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

func applySkipVote(s *discordgo.Session, i *discordgo.InteractionCreate, session *voteSession, skipped *queue.Song) func(int) {
	return func(votes int) {
		skipErr := player.Skip(s, i.GuildID)
		if skipErr != nil && skipErr != player.ErrQueueEmpty {
			errEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, skipErr))
			s.ChannelMessageEditEmbed(session.channelID, session.messageID, errEmbed)
			return
		}

		ClearStopVotes(i.GuildID)

		embed := skipResultEmbed(i.GuildID, skipped, skipErr == player.ErrQueueEmpty)
		messages.AddField(embed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", votes, session.requiredVotes), true)
		s.ChannelMessageEditEmbed(session.channelID, session.messageID, embed)

		if skipErr == player.ErrQueueEmpty {
			player.Stop(i.GuildID)
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

	requiredVotes, err := requiredVotesInChannel(s, i.GuildID, voiceState.ChannelID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	if existing := activeVoteFor(skipVotes, &skipVotesMutex, i.GuildID); existing != nil {
		replyVoteInProgress(s, i, messages.T(i.GuildID).Titles.SkipVote, existing)
		return nil
	}

	skipped := q.Songs[0]

	if requiredVotes == 1 {
		err := player.Skip(s, i.GuildID)
		if err != nil && err != player.ErrQueueEmpty {
			logger.Errorf("Failed to skip: %v", err)
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.SkipFailedTitle,
				fmt.Sprintf(messages.T(i.GuildID).Music.SkipFailedDesc, err)))
			return nil
		}

		ClearStopVotes(i.GuildID)
		UpdateResponseEmbed(s, i, skipResultEmbed(i.GuildID, skipped, err == player.ErrQueueEmpty))

		if err == player.ErrQueueEmpty {
			if stopErr := player.Stop(i.GuildID); stopErr != nil {
				logger.Errorf("Failed to cleanup after queue empty: %v", stopErr)
			}
		}

		return nil
	}

	session := &voteSession{
		votes:          make(map[string]bool),
		requiredVotes:  requiredVotes,
		startTime:      time.Now(),
		cancelTimer:    make(chan bool, 1),
		voiceChannelID: voiceState.ChannelID,
	}
	currentVotes, _ := session.castVote(i.Member.User.ID)

	if existing := claimVoteSession(skipVotes, &skipVotesMutex, i.GuildID, session); existing != nil {
		replyVoteInProgress(s, i, messages.T(i.GuildID).Titles.SkipVote, existing)
		return nil
	}

	embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.SkipVote, "")
	messages.AddField(embed, messages.T(i.GuildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", currentVotes, session.requiredVotes), true)
	messages.SetFooter(embed, fmt.Sprintf(messages.T(i.GuildID).Footers.VoteReaction, "⏭", int(voteExpirationTime.Seconds())))
	UpdateResponseEmbed(s, i, embed)

	msg, msgErr := GetResponseMessage(s, i)
	if msgErr != nil || msg == nil {
		logger.Errorf("Failed to get vote message: %v", msgErr)
		releaseVoteSession(skipVotes, &skipVotesMutex, i.GuildID, session)
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.CommandExecutionError))
		return nil
	}

	session.messageID = msg.ID
	session.channelID = msg.ChannelID

	go startVoteWithReaction(s, i.GuildID, messages.T(i.GuildID).Titles.SkipVote, "⏭", session, skipVotes, &skipVotesMutex, applySkipVote(s, i, session, skipped))

	return nil
}
