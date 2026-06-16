package queue

import (
	"fmt"
	"sync"
	"testing"
)

func seedSongs(t *testing.T, guildID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		song := &Song{
			URL:            fmt.Sprintf("https://youtube.com/watch?v=seed%d", i),
			Title:          fmt.Sprintf("Seed Song %d", i),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
		if err := AddSong(guildID, song, -1); err != nil {
			t.Fatalf("Failed to seed song %d: %v", i, err)
		}
	}
}

func TestSpamRemoveFirstSong(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 2)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RemoveFirstSong(guildID); err != nil {
				t.Errorf("RemoveFirstSong returned error during spam: %v", err)
			}
		}()
	}
	wg.Wait()

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("Failed to get queue: %v", err)
	}
	if q == nil {
		t.Fatal("Queue should still exist after spam removes")
	}
	if len(q.Songs) != 0 {
		t.Errorf("Expected empty queue after 5 removes on 2 songs, got %d", len(q.Songs))
	}
}

func TestConcurrentAddAndRemoveFirst(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 3)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			song := &Song{
				URL:            fmt.Sprintf("https://youtube.com/watch?v=add%d", idx),
				Title:          fmt.Sprintf("Added %d", idx),
				Duration:       "3:00",
				RequestedByID:  "user1",
				RequestedByTag: "User#1234",
			}
			AddSong(guildID, song, -1)
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RemoveFirstSong(guildID)
		}()
	}
	wg.Wait()

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("Failed to get queue: %v", err)
	}
	if q == nil {
		t.Fatal("Queue should still exist")
	}
	if len(q.Songs) < 0 {
		t.Errorf("Negative song count: %d", len(q.Songs))
	}
	for i, s := range q.Songs {
		if s.QueuePosition != i {
			t.Errorf("Position corruption: song at index %d has QueuePosition %d", i, s.QueuePosition)
		}
	}
}

func TestConcurrentSwapAndMove(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 5)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				SwapSongs(guildID, idx%5, (idx+2)%5)
			} else {
				MoveSong(guildID, idx%5, (idx+3)%5)
			}
		}(i)
	}
	wg.Wait()

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("Failed to get queue: %v", err)
	}
	if len(q.Songs) != 5 {
		t.Errorf("Swap/Move should not change count, expected 5 got %d", len(q.Songs))
	}
	seen := make(map[int]bool)
	for i, s := range q.Songs {
		if s.QueuePosition != i {
			t.Errorf("Position corruption at index %d: QueuePosition %d", i, s.QueuePosition)
		}
		if seen[s.ID] {
			t.Errorf("Duplicate song ID %d after concurrent swap/move", s.ID)
		}
		seen[s.ID] = true
	}
}

func TestConcurrentSettings(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 3 {
			case 0:
				SetVolume(guildID, float64((idx%10)*10))
			case 1:
				SetRepeatMode(guildID, idx%3)
			case 2:
				SetSponsorBlock(guildID, idx%2 == 0)
			}
		}(i)
	}
	wg.Wait()

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("Failed to get queue: %v", err)
	}
	if q == nil {
		t.Fatal("Queue should still exist after concurrent settings writes")
	}
}

func TestConcurrentCacheInvalidationAndReads(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 3)

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 3 {
			case 0:
				InvalidateCache(guildID)
			case 1:
				GetQueue(guildID, false)
			case 2:
				GetQueue(guildID, true)
			}
		}(i)
	}
	wg.Wait()

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("Failed to get queue: %v", err)
	}
	if len(q.Songs) != 3 {
		t.Errorf("Expected 3 songs intact, got %d", len(q.Songs))
	}
}
