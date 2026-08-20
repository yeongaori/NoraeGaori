package commands

import "testing"

func newTestVoteSession() *voteSession {
	return newVoteSession("g1", voteKindSkip, "Skip", "⏭", "voice1", 3)
}

func presentBallot(userID string) voteBallot {
	return voteBallot{userID: userID, countsFor: true}
}

func adderBallot(userID string, present bool) voteBallot {
	return voteBallot{userID: userID, countsFor: present, isAdder: true}
}

func TestCastVoteCountsPresentVoters(t *testing.T) {
	vs := newTestVoteSession()

	if !vs.castVote(presentBallot("u1")) {
		t.Error("the first ballot from u1 changed nothing")
	}
	if vs.castVote(presentBallot("u1")) {
		t.Error("a repeated ballot from u1 reported a change")
	}
	if !vs.castVote(presentBallot("u2")) {
		t.Error("the ballot from u2 changed nothing")
	}

	if len(vs.votes) != 2 {
		t.Errorf("counted %d votes, want 2", len(vs.votes))
	}
	if len(vs.adderVotes) != 0 {
		t.Errorf("counted %d adder votes, want 0", len(vs.adderVotes))
	}
}

func TestCastVoteKeepsAbsentAddersOutOfTheQuorum(t *testing.T) {
	vs := newTestVoteSession()

	if !vs.castVote(adderBallot("absent", false)) {
		t.Fatal("the absent adder's ballot changed nothing")
	}

	if len(vs.votes) != 0 {
		t.Errorf("the absent adder raised the quorum count to %d, want 0", len(vs.votes))
	}
	if len(vs.adderVotes) != 1 {
		t.Errorf("counted %d adder votes, want 1", len(vs.adderVotes))
	}
}

func TestCastVoteRecordsPresentAddersInBothSets(t *testing.T) {
	vs := newTestVoteSession()

	vs.castVote(adderBallot("present", true))

	if len(vs.votes) != 1 || len(vs.adderVotes) != 1 {
		t.Errorf("votes=%d adderVotes=%d, want 1 and 1", len(vs.votes), len(vs.adderVotes))
	}
}

func TestWithdrawVoteClearsBothSets(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote(adderBallot("u1", true))
	vs.castVote(presentBallot("u2"))

	if vs.withdrawVote(voteBallot{userID: "u3"}) {
		t.Error("withdrawing a ballot that was never cast reported a change")
	}
	if !vs.withdrawVote(voteBallot{userID: "u1"}) {
		t.Error("withdrawing u1 reported no change")
	}

	if len(vs.votes) != 1 || len(vs.adderVotes) != 0 {
		t.Errorf("votes=%d adderVotes=%d after withdrawal, want 1 and 0", len(vs.votes), len(vs.adderVotes))
	}
	if vs.withdrawVote(voteBallot{userID: "u1"}) {
		t.Error("withdrawing u1 twice reported a change")
	}
}

func TestTallyPassesOnQuorum(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote(presentBallot("u1"))
	vs.castVote(presentBallot("u2"))

	tally := vs.tally(voteThreshold{quorum: 2})
	if !tally.passed {
		t.Error("two of two votes did not pass")
	}
	if tally.byAdderConsent {
		t.Error("a quorum pass was attributed to adder consent")
	}
}

func TestTallyPassesOnAdderConsentBelowQuorum(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote(adderBallot("owner", true))

	tally := vs.tally(voteThreshold{quorum: 3, adders: []string{"owner"}})

	if !tally.passed {
		t.Error("the only requester agreeing did not pass the vote")
	}
	if !tally.byAdderConsent {
		t.Error("the pass was not attributed to adder consent")
	}
	if tally.adderVotes != 1 || tally.adderTotal != 1 {
		t.Errorf("adder counter = %d/%d, want 1/1", tally.adderVotes, tally.adderTotal)
	}
}

func TestTallyWaitsForEveryAdder(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote(adderBallot("ownerA", true))

	tally := vs.tally(voteThreshold{quorum: 4, adders: []string{"ownerA", "ownerB"}})

	if tally.passed {
		t.Error("the vote passed with only one of two requesters agreeing")
	}
	if tally.adderVotes != 1 || tally.adderTotal != 2 {
		t.Errorf("adder counter = %d/%d, want 1/2", tally.adderVotes, tally.adderTotal)
	}
}

func TestTallyDoesNotPassOnAnEmptyAdderSet(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote(presentBallot("u1"))

	tally := vs.tally(voteThreshold{quorum: 3})

	if tally.passed {
		t.Error("an empty adder set passed the vote vacuously")
	}
}

func TestTallyCountsOnlyCurrentAdders(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote(adderBallot("formerOwner", true))

	tally := vs.tally(voteThreshold{quorum: 3, adders: []string{"newOwner"}})

	if tally.adderVotes != 0 {
		t.Errorf("adder votes = %d, want 0 for a requester no longer affected", tally.adderVotes)
	}
	if tally.passed {
		t.Error("a stale adder vote passed the vote")
	}
}

func TestEndWithKeepsOnlyTheFirstReason(t *testing.T) {
	vs := newTestVoteSession()

	vs.endWith(voteEndSuperseded)
	vs.endWith(voteEndExpired)

	if reason := <-vs.done; reason != voteEndSuperseded {
		t.Errorf("done delivered %v, want the first reason %v", reason, voteEndSuperseded)
	}

	select {
	case reason := <-vs.done:
		t.Errorf("done delivered a second reason %v, want nothing", reason)
	default:
	}
}

func TestVoteMessageURL(t *testing.T) {
	want := "https://discord.com/channels/g1/c1/m1"
	if got := voteMessageURL("g1", "c1", "m1"); got != want {
		t.Errorf("voteMessageURL = %q, want %q", got, want)
	}
}
