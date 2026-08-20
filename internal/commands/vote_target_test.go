package commands

import (
	"testing"

	"noraegaori/internal/queue"
)

func queueOf(requesters ...string) *queue.Queue {
	songs := make([]*queue.Song, 0, len(requesters))
	for index, requester := range requesters {
		songs = append(songs, &queue.Song{ID: index + 1, RequestedByID: requester})
	}
	return &queue.Queue{Songs: songs}
}

func requestersOf(songs []*queue.Song) []string {
	ids := make([]string, 0, len(songs))
	for _, song := range songs {
		ids = append(ids, song.RequestedByID)
	}
	return ids
}

func TestAffectedSongsFollowsTheScope(t *testing.T) {
	q := queueOf("a", "b", "c", "b")

	current := affectedSongs(q, voteTarget{scope: voteScopeCurrentSong})
	if len(current) != 1 || current[0].RequestedByID != "a" {
		t.Errorf("current-song scope = %v, want the head of the queue", requestersOf(current))
	}

	whole := affectedSongs(q, voteTarget{scope: voteScopeWholeQueue})
	if len(whole) != 4 {
		t.Errorf("whole-queue scope covered %d songs, want 4", len(whole))
	}

	picked := affectedSongs(q, voteTarget{scope: voteScopeSongs, songIDs: []int{2, 4}})
	if len(picked) != 2 || picked[0].ID != 2 || picked[1].ID != 4 {
		t.Errorf("song scope = %v, want songs 2 and 4", picked)
	}
}

func TestAffectedSongsIgnoresUnknownIDsAndEmptyQueues(t *testing.T) {
	if songs := affectedSongs(nil, voteTarget{scope: voteScopeWholeQueue}); songs != nil {
		t.Errorf("a nil queue produced %v, want nothing", songs)
	}

	if songs := affectedSongs(queueOf(), voteTarget{scope: voteScopeCurrentSong}); songs != nil {
		t.Errorf("an empty queue produced %v, want nothing", songs)
	}

	songs := affectedSongs(queueOf("a", "b"), voteTarget{scope: voteScopeSongs, songIDs: []int{99}})
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

func TestSongIDsAndOwnership(t *testing.T) {
	q := queueOf("a", "b", "a")

	if ids := songIDsOf(q.Songs); len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Errorf("songIDsOf = %v, want 1, 2, 3", ids)
	}

	if !ownedBy(q.Songs[:1], "a") {
		t.Error("a caller's own song was not recognised")
	}
	if ownedBy(q.Songs, "a") {
		t.Error("a range containing another user's song was treated as owned")
	}
	if !ownedBy(nil, "a") {
		t.Error("an empty selection was not treated as owned")
	}
}

func TestRemovableFromDropsVanishedAndPlayingSongs(t *testing.T) {
	q := queueOf("a", "b", "c")
	q.Playing = true

	songs := removableFrom(q, []int{1, 2, 99})
	if len(songs) != 1 || songs[0].ID != 2 {
		t.Errorf("removable = %v, want only song 2: song 1 is playing and 99 is gone", songs)
	}

	if songs := removableFrom(q, []int{99}); len(songs) != 0 {
		t.Errorf("removable = %v, want nothing when every target is gone", songs)
	}

	idle := queueOf("a", "b")
	if songs := removableFrom(idle, []int{1}); len(songs) != 1 {
		t.Errorf("removable = %v, want the head removable while nothing is playing", songs)
	}

	if songs := removableFrom(nil, []int{1}); songs != nil {
		t.Errorf("removable = %v, want nothing from a nil queue", songs)
	}
}
