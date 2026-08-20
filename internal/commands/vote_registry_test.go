package commands

import (
	"sync"
	"testing"
)

func newTestRegistrySession(guildID string, kind voteKind) *voteSession {
	return newVoteSession(guildID, kind, "Vote", "⏭", "voice1", 2)
}

func TestClaimHoldsOneSessionPerKind(t *testing.T) {
	registry := newVoteRegistry()

	first := newTestRegistrySession("g1", voteKindSkip)
	if _, claimed := registry.claim(first); !claimed {
		t.Fatal("the first claim was rejected")
	}

	second := newTestRegistrySession("g1", voteKindSkip)
	snapshot, claimed := registry.claim(second)
	if claimed {
		t.Error("a second claim of the same kind was accepted")
	}
	registry.attachMessage(first, "msg1", "chan1")
	if snapshot, _ = registry.snapshotOf("g1", voteKindSkip); snapshot.messageID != "msg1" {
		t.Errorf("snapshot messageID = %q, want msg1", snapshot.messageID)
	}

	stop := newTestRegistrySession("g1", voteKindStop)
	if _, claimed := registry.claim(stop); !claimed {
		t.Error("a different kind in the same guild was rejected")
	}

	otherGuild := newTestRegistrySession("g2", voteKindSkip)
	if _, claimed := registry.claim(otherGuild); !claimed {
		t.Error("the same kind in another guild was rejected")
	}
}

func TestReleaseOnlyEvictsItsOwnSession(t *testing.T) {
	registry := newVoteRegistry()

	session := newTestRegistrySession("g1", voteKindSkip)
	registry.claim(session)
	registry.attachMessage(session, "msg1", "chan1")

	registry.release(newTestRegistrySession("g1", voteKindSkip))
	if _, live := registry.snapshotOf("g1", voteKindSkip); !live {
		t.Error("release evicted a session it does not own")
	}
	if registry.sessionForMessage("msg1") != session {
		t.Error("release unindexed a message it does not own")
	}

	registry.release(session)
	if _, live := registry.snapshotOf("g1", voteKindSkip); live {
		t.Error("release did not evict its own session")
	}
	if registry.sessionForMessage("msg1") != nil {
		t.Error("release left the message indexed")
	}
}

func TestResolveSucceedsExactlyOnce(t *testing.T) {
	registry := newVoteRegistry()

	session := newTestRegistrySession("g1", voteKindStop)
	registry.claim(session)
	registry.attachMessage(session, "msg1", "chan1")

	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- registry.resolve(session)
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
	if !session.resolved {
		t.Error("the winning resolve did not mark the session resolved")
	}
	if registry.sessionForMessage("msg1") != nil {
		t.Error("resolve left the message indexed")
	}
}

func TestCancelEndsNamedKindsOnly(t *testing.T) {
	registry := newVoteRegistry()

	skip := newTestRegistrySession("g1", voteKindSkip)
	stop := newTestRegistrySession("g1", voteKindStop)
	registry.claim(skip)
	registry.claim(stop)
	registry.attachMessage(skip, "msgSkip", "chan1")
	registry.attachMessage(stop, "msgStop", "chan1")

	registry.cancel("g1", voteEndSuperseded, voteKindSkip)

	if reason := <-skip.done; reason != voteEndSuperseded {
		t.Errorf("skip ended with %v, want voteEndSuperseded", reason)
	}
	if _, live := registry.snapshotOf("g1", voteKindStop); !live {
		t.Error("cancel of one kind evicted the other")
	}
	if registry.sessionForMessage("msgSkip") != nil {
		t.Error("cancel left the message indexed")
	}

	registry.cancel("g1", voteEndQueueEnded)
	if reason := <-stop.done; reason != voteEndQueueEnded {
		t.Errorf("stop ended with %v, want voteEndQueueEnded", reason)
	}
}

func TestCancelOnAQuietGuildDoesNothing(t *testing.T) {
	registry := newVoteRegistry()
	registry.cancel("nobody-here", voteEndCancelled)
}

func TestRecordVoteReportsQuorum(t *testing.T) {
	registry := newVoteRegistry()

	session := newTestRegistrySession("g1", voteKindSkip)
	registry.claim(session)

	tally, counted := registry.recordVote(session, presentBallot("u1"), voteThreshold{quorum: 2})
	if !counted || tally.current != 1 || tally.required != 2 || tally.passed {
		t.Errorf("first vote = %+v counted=%v, want current 1 of 2 and not passed", tally, counted)
	}

	if _, counted := registry.recordVote(session, presentBallot("u1"), voteThreshold{quorum: 2}); counted {
		t.Error("a repeated vote from the same user was counted")
	}

	tally, counted = registry.recordVote(session, presentBallot("u2"), voteThreshold{quorum: 2})
	if !counted || !tally.passed {
		t.Errorf("second voter = %+v counted=%v, want passed", tally, counted)
	}
}

func TestRecordVoteUsesTheQuorumItIsGiven(t *testing.T) {
	registry := newVoteRegistry()

	session := newTestRegistrySession("g1", voteKindSkip)
	registry.claim(session)

	tally, _ := registry.recordVote(session, presentBallot("u1"), voteThreshold{quorum: 1})
	if !tally.passed || tally.required != 1 {
		t.Errorf("tally = %+v, want a passing vote against the recomputed quorum of 1", tally)
	}
}

func TestVotesOnAnEvictedSessionAreRejected(t *testing.T) {
	registry := newVoteRegistry()

	session := newTestRegistrySession("g1", voteKindSkip)
	registry.claim(session)
	registry.resolve(session)

	if _, counted := registry.recordVote(session, presentBallot("u1"), voteThreshold{quorum: 2}); counted {
		t.Error("a vote on a resolved session was counted")
	}
	if _, withdrawn := registry.retractVote(session, voteBallot{userID: "u1"}, voteThreshold{quorum: 2}); withdrawn {
		t.Error("a withdrawal on a resolved session was counted")
	}
}

func TestRetractVoteDecrements(t *testing.T) {
	registry := newVoteRegistry()

	session := newTestRegistrySession("g1", voteKindSkip)
	registry.claim(session)
	registry.recordVote(session, presentBallot("u1"), voteThreshold{quorum: 3})
	registry.recordVote(session, presentBallot("u2"), voteThreshold{quorum: 3})

	tally, withdrawn := registry.retractVote(session, voteBallot{userID: "u2"}, voteThreshold{quorum: 3})
	if !withdrawn || tally.current != 1 {
		t.Errorf("retract = %+v withdrawn=%v, want current 1", tally, withdrawn)
	}

	if _, withdrawn := registry.retractVote(session, voteBallot{userID: "u3"}, voteThreshold{quorum: 3}); withdrawn {
		t.Error("a withdrawal from a non-voter was counted")
	}
}

func TestConcurrentClaimAndSnapshotStayConsistent(t *testing.T) {
	registry := newVoteRegistry()

	var wg sync.WaitGroup
	claims := make(chan bool, 16)

	for index := range 16 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			session := newTestRegistrySession("g1", voteKindSkip)
			snapshot, claimed := registry.claim(session)
			if claimed {
				registry.attachMessage(session, "msg", "chan")
			} else if snapshot.messageID != "" && snapshot.channelID == "" {
				t.Errorf("goroutine %d saw a half-written snapshot: %+v", index, snapshot)
			}
			claims <- claimed
		}(index)
	}

	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, live := registry.snapshotOf("g1", voteKindSkip)
			if live && snapshot.messageID != "" && snapshot.channelID == "" {
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
