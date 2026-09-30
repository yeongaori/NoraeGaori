package vote_test

import (
	"testing"

	"noraegaori/internal/vote"
)

func newTestVoteSession() *vote.Session {
	return vote.HookNewVoteSession("g1", vote.KindSkip, "Skip", "⏭", "voice1", 3)
}

func presentBallot(userID string) vote.HookVoteBallot {
	return *vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: userID, CountsFor: true})
}

func adderBallot(userID string, present bool) vote.HookVoteBallot {
	return *vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: userID, CountsFor: present, IsAdder: true})
}

func TestCastVoteCountsPresentVoters(t *testing.T) {
	vs := newTestVoteSession()

	if !vs.HookCastVote(presentBallot("u1")) {
		t.Error("the first ballot from u1 changed nothing")
	}
	if vs.HookCastVote(presentBallot("u1")) {
		t.Error("a repeated ballot from u1 reported a change")
	}
	if !vs.HookCastVote(presentBallot("u2")) {
		t.Error("the ballot from u2 changed nothing")
	}

	if len(*vs.HookVotes()) != 2 {
		t.Errorf("counted %d votes, want 2", len(*vs.HookVotes()))
	}
	if len(*vs.HookAdderVotes()) != 0 {
		t.Errorf("counted %d adder votes, want 0", len(*vs.HookAdderVotes()))
	}
}

func TestCastVoteKeepsAbsentAddersOutOfTheQuorum(t *testing.T) {
	vs := newTestVoteSession()

	if !vs.HookCastVote(adderBallot("absent", false)) {
		t.Fatal("the absent adder's ballot changed nothing")
	}

	if len(*vs.HookVotes()) != 0 {
		t.Errorf("the absent adder raised the quorum count to %d, want 0", len(*vs.HookVotes()))
	}
	if len(*vs.HookAdderVotes()) != 1 {
		t.Errorf("counted %d adder votes, want 1", len(*vs.HookAdderVotes()))
	}
}

func TestCastVoteRecordsPresentAddersInBothSets(t *testing.T) {
	vs := newTestVoteSession()

	vs.HookCastVote(adderBallot("present", true))

	if len(*vs.HookVotes()) != 1 || len(*vs.HookAdderVotes()) != 1 {
		t.Errorf("votes=%d adderVotes=%d, want 1 and 1", len(*vs.HookVotes()), len(*vs.HookAdderVotes()))
	}
}

func TestWithdrawVoteClearsBothSets(t *testing.T) {
	vs := newTestVoteSession()
	vs.HookCastVote(adderBallot("u1", true))
	vs.HookCastVote(presentBallot("u2"))

	if vs.HookWithdrawVote(*vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u3"})) {
		t.Error("withdrawing a ballot that was never cast reported a change")
	}
	if !vs.HookWithdrawVote(*vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u1"})) {
		t.Error("withdrawing u1 reported no change")
	}

	if len(*vs.HookVotes()) != 1 || len(*vs.HookAdderVotes()) != 0 {
		t.Errorf("votes=%d adderVotes=%d after withdrawal, want 1 and 0", len(*vs.HookVotes()), len(*vs.HookAdderVotes()))
	}
	if vs.HookWithdrawVote(*vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u1"})) {
		t.Error("withdrawing u1 twice reported a change")
	}
}

func TestTallyPassesOnQuorum(t *testing.T) {
	vs := newTestVoteSession()
	vs.HookCastVote(presentBallot("u1"))
	vs.HookCastVote(presentBallot("u2"))

	tally := vs.HookTally(*vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 2}))
	if !*tally.HookPassed() {
		t.Error("two of two votes did not pass")
	}
	if *tally.HookByAdderConsent() {
		t.Error("a quorum pass was attributed to adder consent")
	}
}

func TestTallyPassesOnAdderConsentBelowQuorum(t *testing.T) {
	vs := newTestVoteSession()
	vs.HookCastVote(adderBallot("owner", true))

	tally := vs.HookTally(*vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3, Adders: []string{"owner"}}))

	if !*tally.HookPassed() {
		t.Error("the only requester agreeing did not pass the vote")
	}
	if !*tally.HookByAdderConsent() {
		t.Error("the pass was not attributed to adder consent")
	}
	if *tally.HookAdderVotes() != 1 || *tally.HookAdderTotal() != 1 {
		t.Errorf("adder counter = %d/%d, want 1/1", *tally.HookAdderVotes(), *tally.HookAdderTotal())
	}
}

func TestTallyWaitsForEveryAdder(t *testing.T) {
	vs := newTestVoteSession()
	vs.HookCastVote(adderBallot("ownerA", true))

	tally := vs.HookTally(*vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 4, Adders: []string{"ownerA", "ownerB"}}))

	if *tally.HookPassed() {
		t.Error("the vote passed with only one of two requesters agreeing")
	}
	if *tally.HookAdderVotes() != 1 || *tally.HookAdderTotal() != 2 {
		t.Errorf("adder counter = %d/%d, want 1/2", *tally.HookAdderVotes(), *tally.HookAdderTotal())
	}
}

func TestTallyDoesNotPassOnAnEmptyAdderSet(t *testing.T) {
	vs := newTestVoteSession()
	vs.HookCastVote(presentBallot("u1"))

	tally := vs.HookTally(*vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3}))

	if *tally.HookPassed() {
		t.Error("an empty adder set passed the vote vacuously")
	}
}

func TestTallyCountsOnlyCurrentAdders(t *testing.T) {
	vs := newTestVoteSession()
	vs.HookCastVote(adderBallot("formerOwner", true))

	tally := vs.HookTally(*vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3, Adders: []string{"newOwner"}}))

	if *tally.HookAdderVotes() != 0 {
		t.Errorf("adder votes = %d, want 0 for a requester no longer affected", *tally.HookAdderVotes())
	}
	if *tally.HookPassed() {
		t.Error("a stale adder vote passed the vote")
	}
}

func TestEndWithKeepsOnlyTheFirstReason(t *testing.T) {
	vs := newTestVoteSession()

	vs.HookEndWith(vote.HookVoteEndSuperseded)
	vs.HookEndWith(vote.HookVoteEndExpired)

	if reason := <-*vs.HookDone(); reason != vote.HookVoteEndSuperseded {
		t.Errorf("done delivered %v, want the first reason %v", reason, vote.HookVoteEndSuperseded)
	}

	select {
	case reason := <-*vs.HookDone():
		t.Errorf("done delivered a second reason %v, want nothing", reason)
	default:
	}
}

func TestVoteMessageURL(t *testing.T) {
	want := "https://discord.com/channels/g1/c1/m1"
	if got := vote.HookVoteMessageURL("g1", "c1", "m1"); got != want {
		t.Errorf("voteMessageURL = %q, want %q", got, want)
	}
}
