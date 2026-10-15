package precachetest

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

type Tracker struct {
	guildID string
	songIDs map[string]int
}

func Track(guildID string) *Tracker {
	return &Tracker{guildID: guildID, songIDs: make(map[string]int)}
}

func (tracker *Tracker) CacheQueuedSongs(t *testing.T) {
	t.Helper()

	q, err := queue.GetQueue(tracker.guildID, true)
	if err != nil || q == nil {
		t.Fatalf("queue not ready: %v", err)
	}

	player.HookPreCacheStoreMu.Lock()
	for index := 1; index < len(q.Songs); index++ {
		song := q.Songs[index]
		tracker.songIDs[song.Title] = song.ID
		(*player.HookPreCacheStore)[fmt.Sprintf("%s_%d", tracker.guildID, song.ID)] = &player.PreCache{
			StreamURL:  song.URL,
			SongID:     song.ID,
			Timestamp:  time.Now(),
			CancelFunc: func() {},
		}
	}
	player.HookPreCacheStoreMu.Unlock()
	t.Cleanup(func() { player.ClearPreCache(tracker.guildID) })
}

func (tracker *Tracker) WantCached(want ...string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, _ map[string]any) {
		t.Helper()

		var cached []string
		for title, songID := range tracker.songIDs {
			if player.GetPreCache(tracker.guildID, songID) != nil {
				cached = append(cached, title)
			}
		}
		slices.Sort(cached)
		if !slices.Equal(cached, slices.Sorted(slices.Values(want))) {
			t.Errorf("cached songs = %v, want %v", cached, want)
		}
	}
}
