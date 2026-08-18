package commands

import (
	"fmt"
	"math"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
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

	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.ServerInfoFailed))
		return err
	}

	voiceMembers := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == voiceState.ChannelID {
			member, err := s.State.Member(i.GuildID, vs.UserID)
			if err == nil && !member.User.Bot {
				voiceMembers++
			}
		}
	}

	requiredVotes := int(math.Ceil(float64(voiceMembers) * 0.5))
	if requiredVotes < 1 {
		requiredVotes = 1
	}

	if existing := activeVoteFor(stopVotes, &stopVotesMutex, i.GuildID); existing != nil {
		replyVoteInProgress(s, i, messages.T(i.GuildID).Titles.StopVote, existing)
		return nil
	}

	if requiredVotes == 1 {
		if err := player.Stop(i.GuildID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, err)))
			return nil
		}

		ClearSkipVotes(i.GuildID)

		UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc))
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

	if existing := claimVoteSession(stopVotes, &stopVotesMutex, i.GuildID, session); existing != nil {
		replyVoteInProgress(s, i, messages.T(i.GuildID).Titles.StopVote, existing)
		return nil
	}

	embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.StopVote, "")
	messages.AddField(embed, messages.T(i.GuildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", currentVotes, session.requiredVotes), true)
	messages.SetFooter(embed, fmt.Sprintf(messages.T(i.GuildID).Footers.VoteReaction, "⏹", int(voteExpirationTime.Seconds())))
	UpdateResponseEmbed(s, i, embed)

	msg, msgErr := GetResponseMessage(s, i)
	if msgErr != nil || msg == nil {
		logger.Errorf("Failed to get vote message: %v", msgErr)
		releaseVoteSession(stopVotes, &stopVotesMutex, i.GuildID, session)
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.CommandExecutionError))
		return nil
	}

	session.messageID = msg.ID
	session.channelID = msg.ChannelID

	go startVoteWithReaction(s, i.GuildID, messages.T(i.GuildID).Titles.StopVote, "⏹", session, stopVotes, &stopVotesMutex, func(votes int) {
		if stopErr := player.Stop(i.GuildID); stopErr != nil {
			errEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, stopErr))
			s.ChannelMessageEditEmbed(session.channelID, session.messageID, errEmbed)
			return
		}

		ClearSkipVotes(i.GuildID)

		stopEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc)
		messages.AddField(stopEmbed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", votes, session.requiredVotes), true)
		s.ChannelMessageEditEmbed(session.channelID, session.messageID, stopEmbed)
	})

	return nil
}
