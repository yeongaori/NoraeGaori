package commands

import "github.com/bwmarrin/discordgo"

func RegisterVoteDispatcher(s *discordgo.Session) {
	s.AddHandler(onVoteReactionAdd)
	s.AddHandler(onVoteReactionRemove)
}

func voteForReaction(s *discordgo.Session, userID, messageID, emoji string) *voteSession {
	if s.State == nil || s.State.User == nil || userID == s.State.User.ID {
		return nil
	}

	session := activeVotes.sessionForMessage(messageID)
	if session == nil || session.emoji != emoji {
		return nil
	}
	return session
}

func currentThreshold(s *discordgo.Session, session *voteSession) voteThreshold {
	quorum, err := requiredVotesInChannel(s, session.guildID, session.voiceChannelID, resolveFromCache)
	if err != nil {
		quorum = session.requiredVotes
	}

	return voteThreshold{quorum: quorum, adders: addersFor(session.guildID, session.target)}
}

func onVoteReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	session := voteForReaction(s, r.UserID, r.MessageID, r.Emoji.Name)
	if session == nil {
		return
	}

	threshold := currentThreshold(s, session)

	ballot, eligible := classifyVoter(s, session.guildID, session.voiceChannelID, r.UserID, threshold.adders)
	if !eligible {
		removeUserReaction(s, session.channelID, session.messageID, session.emoji, r.UserID)
		return
	}

	tally, counted := activeVotes.recordVote(session, ballot, threshold)
	if !counted {
		return
	}

	if !tally.passed {
		renderVoteProgress(s, session, tally)
		return
	}

	if !activeVotes.resolve(session) {
		return
	}

	session.onPassed(s, session, tally)
	session.endWith(voteEndPassed)
}

func onVoteReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	session := voteForReaction(s, r.UserID, r.MessageID, r.Emoji.Name)
	if session == nil {
		return
	}

	threshold := currentThreshold(s, session)

	tally, withdrawn := activeVotes.retractVote(session, voteBallot{userID: r.UserID}, threshold)
	if !withdrawn {
		return
	}

	renderVoteProgress(s, session, tally)
}
