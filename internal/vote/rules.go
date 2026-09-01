package vote

func CancelSuperseded(guildID string, passed Kind, queueEnded bool) {
	switch passed {
	case KindStop:
		activeVotes.cancel(guildID, voteEndSuperseded, KindSkip, KindRemove)
	case KindSkip:
		if queueEnded {
			activeVotes.cancel(guildID, voteEndQueueEnded, KindStop, KindRemove)
		}
	}
}

func CancelForNewSong(guildID string) {
	activeVotes.cancel(guildID, voteEndCancelled, KindSkip)
}

func CancelForEndedPlayback(guildID string) {
	activeVotes.cancel(guildID, voteEndQueueEnded)
}
