package commands

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/pkg/logger"
)

func replyVoteInProgress(s *discordgo.Session, i *discordgo.InteractionCreate, title string, snapshot voteSnapshot) {
	description := messages.T(i.GuildID).Votes.InProgress
	if snapshot.messageID != "" && snapshot.channelID != "" {
		description = fmt.Sprintf("%s\n%s", description, voteMessageURL(i.GuildID, snapshot.channelID, snapshot.messageID))
	}

	UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(title, description))
}

type voteRequest struct {
	kind           voteKind
	title          string
	description    string
	emoji          string
	voiceChannelID string
	requiredVotes  int
	target         voteTarget
	onPassed       func(s *discordgo.Session, session *voteSession, tally voteTally)
}

func startVote(s *discordgo.Session, i *discordgo.InteractionCreate, request voteRequest) error {
	session := newVoteSession(i.GuildID, request.kind, request.title, request.emoji, request.voiceChannelID, request.requiredVotes)
	session.description = request.description
	session.target = request.target
	session.onPassed = request.onPassed

	adders := addersFor(i.GuildID, request.target)
	session.castVote(voteBallot{
		userID:    i.Member.User.ID,
		countsFor: true,
		isAdder:   isAdder(adders, i.Member.User.ID),
	})

	if snapshot, claimed := activeVotes.claim(session); !claimed {
		replyVoteInProgress(s, i, request.title, snapshot)
		return nil
	}

	tally := session.tally(voteThreshold{quorum: request.requiredVotes, adders: adders})
	UpdateResponseEmbed(s, i, voteProgressEmbed(i.GuildID, request.title, request.description, request.emoji, session.startTime, tally))

	msg, err := GetResponseMessage(s, i)
	if err != nil || msg == nil {
		logger.Errorf("Failed to get vote message: %v", err)
		activeVotes.release(session)
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.CommandExecutionError))
		return nil
	}

	if !activeVotes.attachMessage(session, msg.ID, msg.ChannelID) {
		session.messageID = msg.ID
		session.channelID = msg.ChannelID
		renderVoteEnded(s, session, endedBeforeAttach(session))
		return nil
	}

	go awaitVoteOutcome(s, session)
	return nil
}

func endedBeforeAttach(session *voteSession) voteEndReason {
	select {
	case reason := <-session.done:
		return reason
	default:
		return voteEndCancelled
	}
}

func awaitVoteOutcome(s *discordgo.Session, session *voteSession) {
	addPromptReaction(s, session.channelID, session.messageID, session.emoji)

	timer := time.NewTimer(voteExpirationTime)
	defer timer.Stop()

	select {
	case reason := <-session.done:
		logger.Debugf("%s vote ended for guild %s", session.title, session.guildID)
		if reason != voteEndPassed {
			renderVoteEnded(s, session, reason)
		}
	case <-timer.C:
		logger.Debugf("%s vote expired for guild %s", session.title, session.guildID)
		if activeVotes.resolve(session) {
			renderVoteEnded(s, session, voteEndExpired)
		}
	}

	clearPromptReactions(s, session.channelID, session.messageID)
}
