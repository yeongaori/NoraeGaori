package commands

import (
	"fmt"
	"math"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

// HandleStop stops playback via majority vote; with 1-2 members stops immediately.
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

	// Solo or duo: stop without a vote.
	if requiredVotes == 1 {
		if err := player.Stop(i.GuildID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, err)))
			return nil
		}
		UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc))
		return nil
	}

	isNewSession := false
	stopVotesMutex.Lock()

	session := stopVotes[i.GuildID]
	if session == nil {
		session = &voteSession{
			votes:          make(map[string]bool),
			requiredVotes:  requiredVotes,
			startTime:      time.Now(),
			cancelTimer:    make(chan bool, 1),
			voiceChannelID: voiceState.ChannelID,
		}
		stopVotes[i.GuildID] = session
		isNewSession = true
	}
	stopVotesMutex.Unlock()

	stopVotesMutex.Lock()
	if session.votes[i.Member.User.ID] {
		stopVotesMutex.Unlock()
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.AlreadyVoted, messages.T(i.GuildID).Music.StopAlreadyVoted))
		return nil
	}

	session.votes[i.Member.User.ID] = true
	currentVotes := len(session.votes)
	stopVotesMutex.Unlock()

	if currentVotes >= requiredVotes {
		select {
		case session.cancelTimer <- true:
		default:
		}

		stopVotesMutex.Lock()
		delete(stopVotes, i.GuildID)
		stopVotesMutex.Unlock()

		if err := player.Stop(i.GuildID); err != nil {
			embed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, err))
			messages.AddField(embed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			UpdateResponseEmbed(s, i, embed)
			return nil
		}

		embed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc)
		messages.AddField(embed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		UpdateResponseEmbed(s, i, embed)
	} else {
		embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.StopVote, "")
		messages.AddField(embed, messages.T(i.GuildID).Fields.CurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		messages.SetFooter(embed, fmt.Sprintf(messages.T(i.GuildID).Footers.VoteReaction, "⏹", int(voteExpirationTime.Seconds())))
		UpdateResponseEmbed(s, i, embed)

		if isNewSession {
			msg, msgErr := GetResponseMessage(s, i)
			if msgErr == nil && msg != nil {
				session.messageID = msg.ID
				session.channelID = msg.ChannelID

				go startVoteWithReaction(s, i.GuildID, messages.T(i.GuildID).Titles.StopVote, "⏹", session, stopVotes, &stopVotesMutex, func(votes int) {
					if stopErr := player.Stop(i.GuildID); stopErr != nil {
						errEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Music.StopFailedTitle, fmt.Sprintf(messages.T(i.GuildID).Music.StopFailedDesc, stopErr))
						s.ChannelMessageEditEmbed(session.channelID, session.messageID, errEmbed)
						return
					}
					stopEmbed := messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.StopSuccessTitle, messages.T(i.GuildID).Music.StopSuccessDesc)
					messages.AddField(stopEmbed, messages.T(i.GuildID).Fields.VoteResult, fmt.Sprintf("%d/%d", votes, requiredVotes), true)
					s.ChannelMessageEditEmbed(session.channelID, session.messageID, stopEmbed)
				})
			}
		}
	}

	return nil
}
