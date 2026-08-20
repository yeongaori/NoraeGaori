package queue

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
