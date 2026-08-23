package vote

import (
	"testing"

	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/queuetest"
)

func requestersOf(songs []*queue.Song) []string {
	ids := make([]string, 0, len(songs))
	for _, song := range songs {
		ids = append(ids, song.RequestedByID)
	}
	return ids
}

func TestAffectedSongsFollowsTheScope(t *testing.T) {
	q := queuetest.QueueOf("a", "b", "c", "b")

	current := AffectedSongs(q, Target{Scope: ScopeCurrentSong})
	if len(current) != 1 || current[0].RequestedByID != "a" {
		t.Errorf("current-song scope = %v, want the head of the queue", requestersOf(current))
	}

	whole := AffectedSongs(q, Target{Scope: ScopeWholeQueue})
	if len(whole) != 4 {
		t.Errorf("whole-queue scope covered %d songs, want 4", len(whole))
	}

	picked := AffectedSongs(q, Target{Scope: ScopeSongs, SongIDs: []int{2, 4}})
	if len(picked) != 2 || picked[0].ID != 2 || picked[1].ID != 4 {
		t.Errorf("song scope = %v, want songs 2 and 4", picked)
	}
}

func TestAffectedSongsIgnoresUnknownIDsAndEmptyQueues(t *testing.T) {
	if songs := AffectedSongs(nil, Target{Scope: ScopeWholeQueue}); songs != nil {
		t.Errorf("a nil queue produced %v, want nothing", songs)
	}

	if songs := AffectedSongs(queuetest.QueueOf(), Target{Scope: ScopeCurrentSong}); songs != nil {
		t.Errorf("an empty queue produced %v, want nothing", songs)
	}

	songs := AffectedSongs(queuetest.QueueOf("a", "b"), Target{Scope: ScopeSongs, SongIDs: []int{99}})
	if len(songs) != 0 {
		t.Errorf("an unknown song id matched %v, want nothing", requestersOf(songs))
	}
}

func TestEveryAdderVotedRefusesAnEmptySet(t *testing.T) {
	votes := map[string]struct{}{"a": {}}

	if everyAdderVoted(votes, nil) {
		t.Error("an empty adder set counted as unanimous consent")
	}
	if !everyAdderVoted(votes, []string{"a"}) {
		t.Error("the only requester having voted was not treated as consent")
	}
	if everyAdderVoted(votes, []string{"a", "b"}) {
		t.Error("a missing requester still counted as consent")
	}
}

func TestIsAdder(t *testing.T) {
	adders := []string{"a", "b"}

	if !isAdder(adders, "b") {
		t.Error("a requester was not recognised")
	}
	if isAdder(adders, "c") {
		t.Error("a non-requester was recognised as one")
	}
	if isAdder(nil, "a") {
		t.Error("an empty set recognised a requester")
	}
}
