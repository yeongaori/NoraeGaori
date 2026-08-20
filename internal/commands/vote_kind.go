package commands

import "noraegaori/internal/messages"

type voteKind int

const (
	voteKindSkip voteKind = iota
	voteKindStop
	voteKindRemove
)

var allVoteKinds = []voteKind{voteKindSkip, voteKindStop, voteKindRemove}

type voteEndReason int

const (
	voteEndExpired voteEndReason = iota
	voteEndCancelled
	voteEndSuperseded
	voteEndQueueEnded
	voteEndPassed
)

func voteEndDescription(guildID string, reason voteEndReason) string {
	t := messages.T(guildID)

	switch reason {
	case voteEndExpired:
		return t.Votes.Expired
	case voteEndSuperseded:
		return t.Votes.Superseded
	case voteEndQueueEnded:
		return t.Votes.QueueEnded
	default:
		return t.Votes.Cancelled
	}
}
