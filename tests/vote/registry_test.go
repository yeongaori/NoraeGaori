package vote_test

import (
	"sync"
	"testing"

	"noraegaori/internal/vote"
)

func newTestRegistrySession(guildID string, kind vote.Kind) *vote.Session {
	return vote.HookNewVoteSession(guildID, kind, "Vote", "⏭", "voice1", 2)
}

func TestClaimHoldsOneSessionPerKind(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	first := newTestRegistrySession("g1", vote.KindSkip)
	if _, claimed := registry.HookClaim(first); !claimed {
		t.Fatal("the first claim was rejected")
	}

	registry.HookAttachMessage(first, "msg1", "chan1")

	second := newTestRegistrySession("g1", vote.KindSkip)
	snapshot, claimed := registry.HookClaim(second)
	if claimed {
		t.Error("a second claim of the same kind was accepted")
	}
	if *snapshot.HookMessageID() != "msg1" || *snapshot.HookChannelID() != "chan1" {
		t.Errorf("the refused claim saw %+v, want the running vote's message", snapshot)
	}

	stop := newTestRegistrySession("g1", vote.KindStop)
	if _, claimed := registry.HookClaim(stop); !claimed {
		t.Error("a different kind in the same guild was rejected")
	}

	otherGuild := newTestRegistrySession("g2", vote.KindSkip)
	if _, claimed := registry.HookClaim(otherGuild); !claimed {
		t.Error("the same kind in another guild was rejected")
	}
}

func TestReleaseOnlyEvictsItsOwnSession(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	session := newTestRegistrySession("g1", vote.KindSkip)
	registry.HookClaim(session)
	registry.HookAttachMessage(session, "msg1", "chan1")

	registry.HookRelease(newTestRegistrySession("g1", vote.KindSkip))
	if _, live := registry.HookSnapshotOf("g1", vote.KindSkip); !live {
		t.Error("release evicted a session it does not own")
	}
	if registry.HookSessionForMessage("msg1") != session {
		t.Error("release unindexed a message it does not own")
	}

	registry.HookRelease(session)
	if _, live := registry.HookSnapshotOf("g1", vote.KindSkip); live {
		t.Error("release did not evict its own session")
	}
	if registry.HookSessionForMessage("msg1") != nil {
		t.Error("release left the message indexed")
	}
}

func TestResolveSucceedsExactlyOnce(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	session := newTestRegistrySession("g1", vote.KindStop)
	registry.HookClaim(session)
	registry.HookAttachMessage(session, "msg1", "chan1")

	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- registry.HookResolve(session)
		}()
	}
	wg.Wait()
	close(results)

	won := 0
	for result := range results {
		if result {
			won++
		}
	}

	if won != 1 {
		t.Errorf("resolve succeeded %d times, want exactly 1", won)
	}
	if !*session.HookResolved() {
		t.Error("the winning resolve did not mark the session resolved")
	}
	if registry.HookSessionForMessage("msg1") != nil {
		t.Error("resolve left the message indexed")
	}
}

func TestCancelEndsNamedKindsOnly(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	skip := newTestRegistrySession("g1", vote.KindSkip)
	stop := newTestRegistrySession("g1", vote.KindStop)
	registry.HookClaim(skip)
	registry.HookClaim(stop)
	registry.HookAttachMessage(skip, "msgSkip", "chan1")
	registry.HookAttachMessage(stop, "msgStop", "chan1")

	registry.HookCancel("g1", vote.HookVoteEndSuperseded, vote.KindSkip)

	if reason := <-*skip.HookDone(); reason != vote.HookVoteEndSuperseded {
		t.Errorf("skip ended with %v, want voteEndSuperseded", reason)
	}
	if _, live := registry.HookSnapshotOf("g1", vote.KindStop); !live {
		t.Error("cancel of one kind evicted the other")
	}
	if registry.HookSessionForMessage("msgSkip") != nil {
		t.Error("cancel left the message indexed")
	}

	registry.HookCancel("g1", vote.HookVoteEndQueueEnded)
	if reason := <-*stop.HookDone(); reason != vote.HookVoteEndQueueEnded {
		t.Errorf("stop ended with %v, want voteEndQueueEnded", reason)
	}
}

func TestRecordVoteReportsQuorum(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	session := newTestRegistrySession("g1", vote.KindSkip)
	registry.HookClaim(session)

	tally, counted := registry.HookRecordVote(session, presentBallot("u1"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 2}))
	if !counted || *tally.HookCurrent() != 1 || *tally.HookRequired() != 2 || *tally.HookPassed() {
		t.Errorf("first vote = %+v counted=%v, want current 1 of 2 and not passed", tally, counted)
	}

	if _, counted := registry.HookRecordVote(session, presentBallot("u1"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 2})); counted {
		t.Error("a repeated vote from the same user was counted")
	}

	tally, counted = registry.HookRecordVote(session, presentBallot("u2"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 2}))
	if !counted || !*tally.HookPassed() {
		t.Errorf("second voter = %+v counted=%v, want passed", tally, counted)
	}
}

func TestVotesOnAnEvictedSessionAreRejected(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	session := newTestRegistrySession("g1", vote.KindSkip)
	registry.HookClaim(session)
	registry.HookResolve(session)

	if _, counted := registry.HookRecordVote(session, presentBallot("u1"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 2})); counted {
		t.Error("a vote on a resolved session was counted")
	}
	if _, withdrawn := registry.HookRetractVote(session, *vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u1"}), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 2})); withdrawn {
		t.Error("a withdrawal on a resolved session was counted")
	}
}

func TestRetractVoteDecrements(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	session := newTestRegistrySession("g1", vote.KindSkip)
	registry.HookClaim(session)
	registry.HookRecordVote(session, presentBallot("u1"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3}))
	registry.HookRecordVote(session, presentBallot("u2"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3}))

	tally, withdrawn := registry.HookRetractVote(session, *vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u2"}), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3}))
	if !withdrawn || *tally.HookCurrent() != 1 {
		t.Errorf("retract = %+v withdrawn=%v, want current 1", tally, withdrawn)
	}

	if _, withdrawn := registry.HookRetractVote(session, *vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u3"}), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 3})); withdrawn {
		t.Error("a withdrawal from a non-voter was counted")
	}
	registry.HookRecordVote(session, presentBallot("u2"), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 1}))
	tally, withdrawn = registry.HookRetractVote(session, *vote.HookBuildVoteBallot(vote.HookVoteBallotFields{UserID: "u2"}), *vote.HookBuildVoteThreshold(vote.HookVoteThresholdFields{Quorum: 1, Adders: []string{"u1"}}))
	if !withdrawn || *tally.HookCurrent() != 1 || *tally.HookPassed() || *tally.HookByAdderConsent() {
		t.Errorf("retract = %+v, want a withdrawal never to pass the vote even when the rest still meet the quorum", tally)
	}
}

func TestConcurrentClaimAndSnapshotStayConsistent(t *testing.T) {
	registry := vote.HookNewVoteRegistry()

	var wg sync.WaitGroup
	claims := make(chan bool, 16)

	for index := range 16 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			session := newTestRegistrySession("g1", vote.KindSkip)
			snapshot, claimed := registry.HookClaim(session)
			if claimed {
				registry.HookAttachMessage(session, "msg", "chan")
			} else if *snapshot.HookMessageID() != "" && *snapshot.HookChannelID() == "" {
				t.Errorf("goroutine %d saw a half-written snapshot: %+v", index, snapshot)
			}
			claims <- claimed
		}(index)
	}

	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, live := registry.HookSnapshotOf("g1", vote.KindSkip)
			if live && *snapshot.HookMessageID() != "" && *snapshot.HookChannelID() == "" {
				t.Errorf("reader saw a half-written snapshot: %+v", snapshot)
			}
		}()
	}

	wg.Wait()
	close(claims)

	won := 0
	for claimed := range claims {
		if claimed {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d goroutines claimed the slot, want exactly 1", won)
	}
}
