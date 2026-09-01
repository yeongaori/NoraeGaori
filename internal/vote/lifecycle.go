package vote

import (
	"fmt"
	"noraegaori/internal/discord"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func replyVoteInProgress(s *discordgo.Session, i *discordgo.InteractionCreate, title string, snapshot voteSnapshot) {
	description := messages.T(i.GuildID).Votes.InProgress
	if snapshot.messageID != "" && snapshot.channelID != "" {
		description = fmt.Sprintf("%s\n%s", description, voteMessageURL(i.GuildID, snapshot.channelID, snapshot.messageID))
	}

	discord.UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(title, description))
}

type Request struct {
	Kind           Kind
	Title          string
	Description    string
	Emoji          string
	VoiceChannelID string
	RequiredVotes  int
	Target         Target
	OnPassed       func(s *discordgo.Session, session *Session, tally Tally)
}

func Start(s *discordgo.Session, i *discordgo.InteractionCreate, request Request) error {
	session := newVoteSession(i.GuildID, request.Kind, request.Title, request.Emoji, request.VoiceChannelID, request.RequiredVotes)
	session.description = request.Description
	session.target = request.Target
	session.onPassed = request.OnPassed

	adders := addersFor(i.GuildID, request.Target)
	session.castVote(voteBallot{
		userID:    i.Member.User.ID,
		countsFor: true,
		isAdder:   isAdder(adders, i.Member.User.ID),
	})

	if snapshot, claimed := activeVotes.claim(session); !claimed {
		replyVoteInProgress(s, i, request.Title, snapshot)
		return nil
	}

	tally := session.tally(voteThreshold{quorum: request.RequiredVotes, adders: adders})
	discord.UpdateResponseEmbed(s, i, voteProgressEmbed(i.GuildID, request.Title, request.Description, request.Emoji, session.startTime, tally))

	msg, err := discord.GetResponseMessage(s, i)
	if err != nil || msg == nil {
		logger.Errorf("Failed to get vote message: %v", err)
		activeVotes.release(session)
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.CommandExecutionError))
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

func endedBeforeAttach(session *Session) voteEndReason {
	select {
	case reason := <-session.done:
		return reason
	default:
		return voteEndCancelled
	}
}

func awaitVoteOutcome(s *discordgo.Session, session *Session) {
	discord.AddPromptReaction(s, session.channelID, session.messageID, session.emoji)

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

	discord.ClearPromptReactions(s, session.channelID, session.messageID)
}
