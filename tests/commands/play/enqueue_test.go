package play_test

import (
	"slices"
	"testing"

	"noraegaori/internal/commands/play"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/queuetest"
)

func TestCheckAvailabilityIndex(t *testing.T) {
	testutil.Swap(t, youtube.GetAvailabilityPool().HookCheckFn(), func(string) (bool, bool, error) { return true, false, nil })
	songs := []*youtube.Song{{URL: "https://youtu.be/first"}, {URL: "https://youtu.be/second"}, {URL: "https://youtu.be/third"}}

	results := play.HookCheckAvailability(songs)
	for i := range songs {
		if results[i].URL != songs[i].URL {
			t.Errorf("result %d = %q, want %q", i, results[i].URL, songs[i].URL)
		}
	}
}

func TestEnqueueAvailableSkips(t *testing.T) {
	const guildID = "enqueue-guild"
	queuetest.Seed(t, guildID, &queue.Song{URL: "https://youtu.be/queued", Title: "Already queued", RequestedByID: "user", RequestedByTag: "user"})

	songs := []*youtube.Song{
		{URL: "https://youtu.be/gone", Title: "Gone"},
		{URL: "https://youtu.be/live", Title: "Live", Duration: "3:00"},
		{URL: "https://youtu.be/queued", Title: "Already queued"},
		{URL: "https://youtu.be/slow", Title: "Slow", Duration: "2:00"},
		{URL: "https://youtu.be/fine", Title: "Fine", Duration: "4:00"},
	}
	results := []youtube.BatchResult{
		{Index: 0, Available: false, Error: "Video unavailable"},
		{Index: 1, Available: true, IsLive: true},
		{Index: 2, Available: true},
		{Index: 3, Available: false, Error: "network timeout"},
		{Index: 4, Available: true},
	}

	added, skipped, skippedURLs := play.HookEnqueueAvailable(guildID, songs, results)

	if added != 3 {
		t.Errorf("added: got %d, want %d", added, 3)
	}
	if skipped != 2 {
		t.Errorf("skipped: got %d, want %d", skipped, 2)
	}
	if want := []string{"https://youtu.be/gone"}; !slices.Equal(skippedURLs, want) {
		t.Errorf("skip notices: got %v, want %v", skippedURLs, want)
	}
	if live := songs[1]; live.Duration != youtube.LiveDuration || !live.IsLive {
		t.Errorf("live song: got (%q, %v), want (%q, true)", live.Duration, live.IsLive, youtube.LiveDuration)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	titles := make([]string, 0, len(q.Songs))
	for _, song := range q.Songs {
		titles = append(titles, song.Title)
	}
	if want := []string{"Already queued", "Live", "Slow", "Fine"}; !slices.Equal(titles, want) {
		t.Errorf("queue: got %v, want %v", titles, want)
	}
}
