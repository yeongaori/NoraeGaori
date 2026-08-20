package vote

import "noraegaori/internal/messages"

type Kind int

const (
	KindSkip Kind = iota
	KindStop
	KindRemove
)

var allVoteKinds = []Kind{KindSkip, KindStop, KindRemove}

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
