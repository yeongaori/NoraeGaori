package commands

func cancelSupersededVotes(guildID string, passed voteKind, queueEnded bool) {
	switch passed {
	case voteKindStop:
		activeVotes.cancel(guildID, voteEndSuperseded, voteKindSkip, voteKindRemove)
	case voteKindSkip:
		if queueEnded {
			activeVotes.cancel(guildID, voteEndQueueEnded, voteKindStop, voteKindRemove)
		}
	}
}

func CancelVotesForNewSong(guildID string) {
	activeVotes.cancel(guildID, voteEndCancelled, voteKindSkip)
}

func CancelVotesForEndedPlayback(guildID string) {
	activeVotes.cancel(guildID, voteEndQueueEnded)
}
