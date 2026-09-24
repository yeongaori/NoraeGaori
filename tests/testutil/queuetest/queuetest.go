package queuetest

import (
	"fmt"
	"strings"
	"testing"

	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/dbtest"
)

func QueueOf(requesters ...string) *queue.Queue {
	songs := make([]*queue.Song, 0, len(requesters))
	for index, requester := range requesters {
		songs = append(songs, &queue.Song{ID: index + 1, RequestedByID: requester})
	}
	return &queue.Queue{Songs: songs}
}

func Song(title, requesterID string) *queue.Song {
	return &queue.Song{
		URL:            "https://example.invalid/watch?v=" + strings.ReplaceAll(title, " ", "_"),
		Title:          title,
		Duration:       "3:00",
		RequestedByID:  requesterID,
		RequestedByTag: requesterID + "#0001",
	}
}

func Songs(requesterIDs ...string) []*queue.Song {
	songs := make([]*queue.Song, 0, len(requesterIDs))
	for index, requesterID := range requesterIDs {
		songs = append(songs, Song(fmt.Sprintf("Song %d", index+1), requesterID))
	}
	return songs
}

func SongsBy(requesterID string, count int) []*queue.Song {
	requesterIDs := make([]string, count)
	for index := range requesterIDs {
		requesterIDs[index] = requesterID
	}
	return Songs(requesterIDs...)
}

func Seed(t *testing.T, guildID string, songs ...*queue.Song) *queue.Queue {
	t.Helper()
	return SeedWithVoice(t, guildID, "", songs...)
}

func SeedWithVoice(t *testing.T, guildID, voiceChannelID string, songs ...*queue.Song) *queue.Queue {
	t.Helper()

	dbtest.Setup(t)
	if err := queue.CreateQueue(guildID, "text", voiceChannelID); err != nil {
		t.Fatalf("failed to create the queue: %v", err)
	}
	for _, song := range songs {
		if err := queue.AddSong(guildID, song, -1); err != nil {
			t.Fatalf("failed to add %q: %v", song.Title, err)
		}
	}

	seeded, err := queue.GetQueue(guildID, true)
	if err != nil || seeded == nil {
		t.Fatalf("failed to load the seeded queue: %v", err)
	}
	return seeded
}
