package vote_test

import (
	"testing"

	"noraegaori/internal/vote"
)

func seedVote(t *testing.T, guildID string, kind vote.Kind) *vote.Session {
	t.Helper()

	session := vote.HookNewVoteSession(guildID, kind, "Vote", "⏭", "voice1", 2)
	if _, claimed := (*vote.HookActiveVotes).HookClaim(session); !claimed {
		t.Fatalf("failed to seed a %v vote for guild %s", kind, guildID)
	}
	t.Cleanup(func() { (*vote.HookActiveVotes).HookRelease(session) })
	return session
}

func endReasonOf(t *testing.T, session *vote.Session) (vote.HookVoteEndReason, bool) {
	t.Helper()

	select {
	case reason := <-*session.HookDone():
		return reason, true
	default:
		return vote.HookVoteEndExpired, false
	}
}

func TestStopVotePassingSupersedesTheSkipVote(t *testing.T) {
	skip := seedVote(t, "rules-stop", vote.KindSkip)

	vote.CancelSuperseded("rules-stop", vote.KindStop, false)

	reason, ended := endReasonOf(t, skip)
	if !ended || reason != vote.HookVoteEndSuperseded {
		t.Errorf("skip vote ended=%v reason=%v, want voteEndSuperseded", ended, reason)
	}
}

func TestSkipVotePassingEndsTheStopVoteOnlyWhenTheQueueEmpties(t *testing.T) {
	stop := seedVote(t, "rules-skip-alive", vote.KindStop)

	vote.CancelSuperseded("rules-skip-alive", vote.KindSkip, false)

	if _, ended := endReasonOf(t, stop); ended {
		t.Error("a skip that left songs in the queue ended the stop vote")
	}

	drained := seedVote(t, "rules-skip-drained", vote.KindStop)

	vote.CancelSuperseded("rules-skip-drained", vote.KindSkip, true)

	reason, ended := endReasonOf(t, drained)
	if !ended || reason != vote.HookVoteEndQueueEnded {
		t.Errorf("stop vote ended=%v reason=%v, want voteEndQueueEnded", ended, reason)
	}
}

func TestCancelSkipVotesLeavesTheStopVoteAlone(t *testing.T) {
	skip := seedVote(t, "rules-new-song", vote.KindSkip)
	stop := seedVote(t, "rules-new-song", vote.KindStop)

	vote.CancelSkipVotes("rules-new-song")

	reason, ended := endReasonOf(t, skip)
	if !ended || reason != vote.HookVoteEndCancelled {
		t.Errorf("skip vote ended=%v reason=%v, want voteEndCancelled", ended, reason)
	}
	if _, ended := endReasonOf(t, stop); ended {
		t.Error("cancelling skip votes ended the stop vote")
	}
}

func TestCancelVotesForEndedPlaybackEndsEveryKind(t *testing.T) {
	skip := seedVote(t, "rules-playback", vote.KindSkip)
	stop := seedVote(t, "rules-playback", vote.KindStop)

	vote.CancelForEndedPlayback("rules-playback")

	for _, session := range []*vote.Session{skip, stop} {
		reason, ended := endReasonOf(t, session)
		if !ended || reason != vote.HookVoteEndQueueEnded {
			t.Errorf("%v vote ended=%v reason=%v, want voteEndQueueEnded", *session.HookKind(), ended, reason)
		}
	}
}

func TestVoteEndDescriptionCoversEveryReason(t *testing.T) {
	cases := []struct {
		reason vote.HookVoteEndReason
		want   string
	}{
		{vote.HookVoteEndExpired, "The vote has expired."},
		{vote.HookVoteEndCancelled, "The vote has been cancelled."},
		{vote.HookVoteEndSuperseded, "The stop vote passed, so this vote has ended."},
		{vote.HookVoteEndQueueEnded, "The queue is empty, so this vote has ended."},
	}

	for _, testCase := range cases {
		if got := vote.HookVoteEndDescription("", testCase.reason); got != testCase.want {
			t.Errorf("voteEndDescription(%v) = %q, want %q", testCase.reason, got, testCase.want)
		}
	}
}
