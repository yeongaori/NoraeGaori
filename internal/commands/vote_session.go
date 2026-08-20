package commands

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
)

var voteExpirationTime = 60 * time.Second

type voteBallot struct {
	userID    string
	countsFor bool
	isAdder   bool
}

type voteThreshold struct {
	quorum int
	adders []string
}

type voteTally struct {
	current        int
	required       int
	adderVotes     int
	adderTotal     int
	passed         bool
	byAdderConsent bool
}

type voteSnapshot struct {
	messageID string
	channelID string
}

type voteSession struct {
	guildID        string
	kind           voteKind
	title          string
	description    string
	emoji          string
	voiceChannelID string
	requiredVotes  int
	target         voteTarget
	startTime      time.Time
	done           chan voteEndReason
	onPassed       func(s *discordgo.Session, session *voteSession, tally voteTally)

	votes      map[string]struct{}
	adderVotes map[string]struct{}
	resolved   bool
	messageID  string
	channelID  string
}

func newVoteSession(guildID string, kind voteKind, title, emoji, voiceChannelID string, requiredVotes int) *voteSession {
	return &voteSession{
		guildID:        guildID,
		kind:           kind,
		title:          title,
		emoji:          emoji,
		voiceChannelID: voiceChannelID,
		requiredVotes:  requiredVotes,
		startTime:      time.Now(),
		done:           make(chan voteEndReason, 1),
		votes:          make(map[string]struct{}, requiredVotes),
		adderVotes:     make(map[string]struct{}, 4),
	}
}

func (vs *voteSession) castVote(ballot voteBallot) bool {
	changed := false

	if ballot.countsFor {
		if _, voted := vs.votes[ballot.userID]; !voted {
			vs.votes[ballot.userID] = struct{}{}
			changed = true
		}
	}

	if ballot.isAdder {
		if _, voted := vs.adderVotes[ballot.userID]; !voted {
			vs.adderVotes[ballot.userID] = struct{}{}
			changed = true
		}
	}

	return changed
}

func (vs *voteSession) withdrawVote(ballot voteBallot) bool {
	changed := false

	if _, voted := vs.votes[ballot.userID]; voted {
		delete(vs.votes, ballot.userID)
		changed = true
	}

	if _, voted := vs.adderVotes[ballot.userID]; voted {
		delete(vs.adderVotes, ballot.userID)
		changed = true
	}

	return changed
}

func (vs *voteSession) tally(threshold voteThreshold) voteTally {
	consented := everyAdderVoted(vs.adderVotes, threshold.adders)
	current := len(vs.votes)

	return voteTally{
		current:        current,
		required:       threshold.quorum,
		adderVotes:     vs.countAdderVotes(threshold.adders),
		adderTotal:     len(threshold.adders),
		passed:         current >= threshold.quorum || consented,
		byAdderConsent: consented && current < threshold.quorum,
	}
}

func (vs *voteSession) countAdderVotes(adders []string) int {
	counted := 0
	for _, adder := range adders {
		if _, voted := vs.adderVotes[adder]; voted {
			counted++
		}
	}
	return counted
}

func (vs *voteSession) snapshot() voteSnapshot {
	return voteSnapshot{messageID: vs.messageID, channelID: vs.channelID}
}

func (vs *voteSession) endWith(reason voteEndReason) {
	select {
	case vs.done <- reason:
	default:
	}
}

func voteMessageURL(guildID, channelID, messageID string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}
