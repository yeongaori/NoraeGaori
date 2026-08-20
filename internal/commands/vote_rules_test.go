package commands

import "testing"

func seedVote(t *testing.T, guildID string, kind voteKind) *voteSession {
	t.Helper()

	session := newVoteSession(guildID, kind, "Vote", "⏭", "voice1", 2)
	if _, claimed := activeVotes.claim(session); !claimed {
		t.Fatalf("failed to seed a %v vote for guild %s", kind, guildID)
	}
	t.Cleanup(func() { activeVotes.release(session) })
	return session
}

func endReasonOf(t *testing.T, session *voteSession) (voteEndReason, bool) {
	t.Helper()

	select {
	case reason := <-session.done:
		return reason, true
	default:
		return voteEndExpired, false
	}
}

func TestStopVotePassingSupersedesTheSkipVote(t *testing.T) {
	skip := seedVote(t, "rules-stop", voteKindSkip)

	cancelSupersededVotes("rules-stop", voteKindStop, false)

	reason, ended := endReasonOf(t, skip)
	if !ended || reason != voteEndSuperseded {
		t.Errorf("skip vote ended=%v reason=%v, want voteEndSuperseded", ended, reason)
	}
}

func TestSkipVotePassingEndsTheStopVoteOnlyWhenTheQueueEmpties(t *testing.T) {
	stop := seedVote(t, "rules-skip-alive", voteKindStop)

	cancelSupersededVotes("rules-skip-alive", voteKindSkip, false)

	if _, ended := endReasonOf(t, stop); ended {
		t.Error("a skip that left songs in the queue ended the stop vote")
	}

	drained := seedVote(t, "rules-skip-drained", voteKindStop)

	cancelSupersededVotes("rules-skip-drained", voteKindSkip, true)

	reason, ended := endReasonOf(t, drained)
	if !ended || reason != voteEndQueueEnded {
		t.Errorf("stop vote ended=%v reason=%v, want voteEndQueueEnded", ended, reason)
	}
}

func TestCancelVotesForNewSongLeavesTheStopVoteAlone(t *testing.T) {
	skip := seedVote(t, "rules-new-song", voteKindSkip)
	stop := seedVote(t, "rules-new-song", voteKindStop)

	CancelVotesForNewSong("rules-new-song")

	reason, ended := endReasonOf(t, skip)
	if !ended || reason != voteEndCancelled {
		t.Errorf("skip vote ended=%v reason=%v, want voteEndCancelled", ended, reason)
	}
	if _, ended := endReasonOf(t, stop); ended {
		t.Error("a new song ended the stop vote")
	}
}

func TestCancelVotesForEndedPlaybackEndsEveryKind(t *testing.T) {
	skip := seedVote(t, "rules-playback", voteKindSkip)
	stop := seedVote(t, "rules-playback", voteKindStop)

	CancelVotesForEndedPlayback("rules-playback")

	for _, session := range []*voteSession{skip, stop} {
		reason, ended := endReasonOf(t, session)
		if !ended || reason != voteEndQueueEnded {
			t.Errorf("%v vote ended=%v reason=%v, want voteEndQueueEnded", session.kind, ended, reason)
		}
	}
}

func TestVoteEndDescriptionCoversEveryReason(t *testing.T) {
	cases := []struct {
		reason voteEndReason
		want   string
	}{
		{voteEndExpired, "The vote has expired."},
		{voteEndCancelled, "The vote has been cancelled."},
		{voteEndSuperseded, "The stop vote passed, so this vote has ended."},
		{voteEndQueueEnded, "The queue is empty, so this vote has ended."},
	}

	for _, testCase := range cases {
		if got := voteEndDescription("", testCase.reason); got != testCase.want {
			t.Errorf("voteEndDescription(%v) = %q, want %q", testCase.reason, got, testCase.want)
		}
	}
}
