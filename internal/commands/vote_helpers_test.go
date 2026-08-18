package commands

import (
	"sync"
	"testing"
)

func newTestVoteSession() *voteSession {
	return &voteSession{
		votes:         make(map[string]bool),
		requiredVotes: 3,
	}
}

func TestCastVote(t *testing.T) {
	vs := newTestVoteSession()

	if count, counted := vs.castVote("u1"); count != 1 || !counted {
		t.Errorf("castVote(u1) = (%d, %v), want (1, true)", count, counted)
	}
	if count, counted := vs.castVote("u1"); count != 1 || counted {
		t.Errorf("castVote(u1) repeated = (%d, %v), want (1, false)", count, counted)
	}
	if count, counted := vs.castVote("u2"); count != 2 || !counted {
		t.Errorf("castVote(u2) = (%d, %v), want (2, true)", count, counted)
	}
}

func TestWithdrawVote(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote("u1")
	vs.castVote("u2")

	if count, withdrawn := vs.withdrawVote("u3"); count != 2 || withdrawn {
		t.Errorf("withdrawVote(u3) = (%d, %v), want (2, false)", count, withdrawn)
	}
	if count, withdrawn := vs.withdrawVote("u2"); count != 1 || !withdrawn {
		t.Errorf("withdrawVote(u2) = (%d, %v), want (1, true)", count, withdrawn)
	}
	if count, withdrawn := vs.withdrawVote("u2"); count != 1 || withdrawn {
		t.Errorf("withdrawVote(u2) repeated = (%d, %v), want (1, false)", count, withdrawn)
	}
	if count, withdrawn := vs.withdrawVote("u1"); count != 0 || !withdrawn {
		t.Errorf("withdrawVote(u1) = (%d, %v), want (0, true)", count, withdrawn)
	}
}

func TestVoteMessageURL(t *testing.T) {
	want := "https://discord.com/channels/g1/c1/m1"
	if got := voteMessageURL("g1", "c1", "m1"); got != want {
		t.Errorf("voteMessageURL = %q, want %q", got, want)
	}
}

func TestClaimVoteSession(t *testing.T) {
	votes := make(map[string]*voteSession)
	var mutex sync.RWMutex

	first := newTestVoteSession()
	if existing := claimVoteSession(votes, &mutex, "g1", first); existing != nil {
		t.Errorf("claimVoteSession on empty map = %v, want nil", existing)
	}
	if votes["g1"] != first {
		t.Error("claimVoteSession did not store the session")
	}

	second := newTestVoteSession()
	if existing := claimVoteSession(votes, &mutex, "g1", second); existing != first {
		t.Errorf("claimVoteSession on claimed guild = %v, want the first session", existing)
	}
	if votes["g1"] != first {
		t.Error("claimVoteSession overwrote the live session")
	}

	if existing := claimVoteSession(votes, &mutex, "g2", second); existing != nil {
		t.Errorf("claimVoteSession for another guild = %v, want nil", existing)
	}
}

func TestActiveVoteForAndRelease(t *testing.T) {
	votes := make(map[string]*voteSession)
	var mutex sync.RWMutex

	if got := activeVoteFor(votes, &mutex, "g1"); got != nil {
		t.Errorf("activeVoteFor on empty map = %v, want nil", got)
	}

	session := newTestVoteSession()
	claimVoteSession(votes, &mutex, "g1", session)
	if got := activeVoteFor(votes, &mutex, "g1"); got != session {
		t.Errorf("activeVoteFor = %v, want the claimed session", got)
	}

	releaseVoteSession(votes, &mutex, "g1", newTestVoteSession())
	if got := activeVoteFor(votes, &mutex, "g1"); got != session {
		t.Error("releaseVoteSession removed a session it does not own")
	}

	releaseVoteSession(votes, &mutex, "g1", session)
	if got := activeVoteFor(votes, &mutex, "g1"); got != nil {
		t.Errorf("activeVoteFor after release = %v, want nil", got)
	}
}

func TestCastVoteAfterWithdraw(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote("u1")
	vs.withdrawVote("u1")

	if count, counted := vs.castVote("u1"); count != 1 || !counted {
		t.Errorf("castVote(u1) after withdraw = (%d, %v), want (1, true)", count, counted)
	}
}
