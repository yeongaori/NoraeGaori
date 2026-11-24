package queue_test

import (
	"errors"
	"slices"
	"testing"

	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/queuetest"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	dbtest.Setup(t)
	if err := queue.CreateQueue("guild1", "text_channel_test", "voice_channel_test"); err != nil {
		t.Fatalf("Failed to create test queue: %v", err)
	}
}

type addSongCase struct {
	name      string
	guildID   string
	song      *queue.Song
	position  int
	expectErr bool
}

func TestAddSong(t *testing.T) {
	setupTestDB(t)

	testCases := addSongCases()

	for i := range testCases {
		tc := &testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			err := queue.AddSong(tc.guildID, tc.song, tc.position)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}

	t.Run("Verify queue order", func(t *testing.T) {
		q, err := queue.GetQueue("guild1", false)
		if err != nil {
			t.Fatalf("Failed to get queue: %v", err)
		}
		if len(q.Songs) != 3 {
			t.Errorf("Expected 3 songs, got %d", len(q.Songs))
		}
	})
}

func addSongCases() []addSongCase {
	return []addSongCase{
		{
			name:    "Add song to end",
			guildID: "guild1",
			song: &queue.Song{
				URL:            "https://youtube.com/watch?v=test1",
				Title:          "Test Song 1",
				Duration:       "3:30",
				Thumbnail:      "https://i.ytimg.com/vi/test1/default.jpg",
				RequestedByID:  "user1",
				RequestedByTag: "User#1234",
				Uploader:       "Test Channel",
				IsLive:         false,
			},
			position:  -1,
			expectErr: false,
		},
		{
			name:    "Add song to beginning",
			guildID: "guild1",
			song: &queue.Song{
				URL:            "https://youtube.com/watch?v=test2",
				Title:          "Test Song 2",
				Duration:       "4:20",
				Thumbnail:      "https://i.ytimg.com/vi/test2/default.jpg",
				RequestedByID:  "user2",
				RequestedByTag: "User#5678",
				Uploader:       "Test Channel 2",
				IsLive:         false,
			},
			position:  0,
			expectErr: false,
		},
		{
			name:    "Add live stream",
			guildID: "guild1",
			song: &queue.Song{
				URL:            "https://youtube.com/watch?v=live1",
				Title:          "Live Stream",
				Duration:       "LIVE",
				Thumbnail:      "https://i.ytimg.com/vi/live1/default.jpg",
				RequestedByID:  "user3",
				RequestedByTag: "User#9999",
				Uploader:       "Live Channel",
				IsLive:         true,
			},
			position:  -1,
			expectErr: false,
		},
	}
}

func TestRemoveSong(t *testing.T) {
	guildID := "guild1"
	queuetest.Seed(t, guildID, queuetest.SongsBy("user1", 5)...)

	testCases := []struct {
		name      string
		position  int
		expectErr bool
	}{
		{"Remove valid position", 2, false},
		{"Remove negative position", -1, true},
		{"Remove out of bounds", 100, true},
	}

	for i := range testCases {
		tc := &testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			err := queue.RemoveSong(guildID, tc.position)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func queueTitles(t *testing.T, guildID string) []string {
	t.Helper()

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	titles := make([]string, 0, len(q.Songs))
	for _, song := range q.Songs {
		titles = append(titles, song.Title)
	}
	return titles
}

func TestSwapSongsByIDs(t *testing.T) {
	titles := []string{"Song 1", "Song 2", "Song 3", "Song 4"}
	cases := []struct {
		name       string
		first      int
		second     int
		isPlaying  bool
		isLoading  bool
		wantErr    error
		wantTitles []string
	}{
		{name: "Swap valid songs", first: 0, second: 2, wantTitles: []string{"Song 3", "Song 2", "Song 1", "Song 4"}},
		{name: "Swap the same song", first: 1, second: 1, wantTitles: titles},
		{name: "Swap a missing song", first: 0, second: 4, wantErr: queue.ErrSongNotInQueue, wantTitles: titles},
		{name: "Swap the current song while playing", first: 0, second: 1, isPlaying: true, wantErr: queue.ErrSongIsCurrent, wantTitles: titles},
		{name: "Swap with the current song while playing", first: 1, second: 0, isPlaying: true, wantErr: queue.ErrSongIsCurrent, wantTitles: titles},
		{name: "Swap the current song while loading", first: 0, second: 1, isLoading: true, wantErr: queue.ErrSongIsCurrent, wantTitles: titles},
		{name: "Swap the current song while paused", first: 0, second: 1, wantTitles: []string{"Song 2", "Song 1", "Song 3", "Song 4"}},
	}

	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			guildID := "guild1"
			seeded := queuetest.Seed(t, guildID, queuetest.SongsBy("user1", len(titles))...)
			ids := make([]int, len(seeded.Songs)+1)
			for i, song := range seeded.Songs {
				ids[i] = song.ID
			}
			if testCase.isPlaying {
				if err := queue.SetPlaying(guildID, true); err != nil {
					t.Fatalf("failed to update the queue state: %v", err)
				}
			}
			if testCase.isLoading {
				if err := queue.SetLoading(guildID, true); err != nil {
					t.Fatalf("failed to update the queue state: %v", err)
				}
			}

			if err := queue.SwapSongsByIDs(guildID, ids[testCase.first], ids[testCase.second]); !errors.Is(err, testCase.wantErr) {
				t.Errorf("SwapSongsByIDs returned %v, want %v", err, testCase.wantErr)
			}

			if got := queueTitles(t, guildID); !slices.Equal(got, testCase.wantTitles) {
				t.Errorf("queue = %v, want %v", got, testCase.wantTitles)
			}
		})
	}
}

func TestMoveSong(t *testing.T) {
	guildID := "guild1"
	queuetest.Seed(t, guildID, queuetest.SongsBy("user1", 5)...)

	testCases := []struct {
		name      string
		from      int
		to        int
		expectErr bool
	}{
		{"Move down", 0, 3, false},
		{"Move up", 4, 1, false},
		{"Move same position", 2, 2, false},
		{"Move out of bounds", 0, 100, true},
	}

	for i := range testCases {
		tc := &testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			err := queue.MoveSong(guildID, tc.from, tc.to)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestSetVolume(t *testing.T) {
	setupTestDB(t)

	guildID := "guild1"

	testCases := []struct {
		name      string
		volume    float64
		expectErr bool
	}{
		{"Valid volume 50", 50, false},
		{"Valid volume 0", 0, false},
		{"Valid volume 1000", 1000, false},
		{"Invalid negative volume", -10, true},
		{"Invalid volume > 1000", 1500, true},
	}

	for i := range testCases {
		tc := &testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			err := queue.SetVolume(guildID, tc.volume)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectErr {
				q, _ := queue.GetQueue(guildID, false)
				if q.Volume != tc.volume {
					t.Errorf("Expected volume %.0f, got %.0f", tc.volume, q.Volume)
				}
			}
		})
	}
}

func TestSetRepeatMode(t *testing.T) {
	setupTestDB(t)

	guildID := "guild1"

	if err := queue.SetRepeatMode(guildID, queue.RepeatAll); err != nil {
		t.Errorf("Failed to set RepeatAll: %v", err)
	}
	q, _ := queue.GetQueue(guildID, false)
	if q.RepeatMode != queue.RepeatAll {
		t.Errorf("Expected queue.RepeatAll (%d), got %d", queue.RepeatAll, q.RepeatMode)
	}

	if err := queue.SetRepeatMode(guildID, queue.RepeatSingle); err != nil {
		t.Errorf("Failed to set RepeatSingle: %v", err)
	}
	q, _ = queue.GetQueue(guildID, false)
	if q.RepeatMode != queue.RepeatSingle {
		t.Errorf("Expected queue.RepeatSingle (%d), got %d", queue.RepeatSingle, q.RepeatMode)
	}

	if err := queue.SetRepeatMode(guildID, queue.RepeatOff); err != nil {
		t.Errorf("Failed to set RepeatOff: %v", err)
	}
	q, _ = queue.GetQueue(guildID, false)
	if q.RepeatMode != queue.RepeatOff {
		t.Errorf("Expected queue.RepeatOff (%d), got %d", queue.RepeatOff, q.RepeatMode)
	}
}

func checkToggle(t *testing.T, name string, set func(guildID string, enabled bool) error, get func(q *queue.Queue) bool) {
	t.Helper()

	setupTestDB(t)

	guildID := "guild1"

	if err := set(guildID, true); err != nil {
		t.Errorf("Failed to enable %s: %v", name, err)
	}

	q, _ := queue.GetQueue(guildID, false)
	if !get(q) {
		t.Errorf("%s should be enabled", name)
	}

	if err := set(guildID, false); err != nil {
		t.Errorf("Failed to disable %s: %v", name, err)
	}

	q, _ = queue.GetQueue(guildID, false)
	if get(q) {
		t.Errorf("%s should be disabled", name)
	}
}

func TestSetSponsorBlock(t *testing.T) {
	checkToggle(t, "SponsorBlock", queue.SetSponsorBlock, func(q *queue.Queue) bool { return q.SponsorBlock })
}

func TestSetShowStartedTrack(t *testing.T) {
	checkToggle(t, "ShowStartedTrack", queue.SetShowStartedTrack, func(q *queue.Queue) bool { return q.ShowStartedTrack })
}

func TestSetNormalization(t *testing.T) {
	checkToggle(t, "Normalization", queue.SetNormalization, func(q *queue.Queue) bool { return q.Normalization })
}

func TestCacheInvalidation(t *testing.T) {
	setupTestDB(t)

	guildID := "guild1"

	song := &queue.Song{
		URL:            "https://youtube.com/watch?v=test",
		Title:          "Test Song",
		Duration:       "3:00",
		RequestedByID:  "user1",
		RequestedByTag: "User#1234",
	}
	queue.AddSong(guildID, song, -1)

	q1, _ := queue.GetQueue(guildID, false)

	queue.InvalidateCache(guildID)

	q2, _ := queue.GetQueue(guildID, false)

	if len(q1.Songs) != len(q2.Songs) {
		t.Error("Cache invalidation affected queue content")
	}
}

func TestConcurrentAccess(t *testing.T) {
	setupTestDB(t)

	guildID := "guild1"
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			song := &queue.Song{
				URL:            "https://youtube.com/watch?v=concurrent" + string(rune('0'+idx)),
				Title:          "Concurrent Song",
				Duration:       "3:00",
				RequestedByID:  "user1",
				RequestedByTag: "User#1234",
			}
			queue.AddSong(guildID, song, -1)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	q, _ := queue.GetQueue(guildID, false)
	if len(q.Songs) != 10 {
		t.Errorf("Expected 10 songs after concurrent adds, got %d", len(q.Songs))
	}
}

func TestUpdateVoiceChannel(t *testing.T) {
	setupTestDB(t)

	guildID := "guild1"
	channelID := "voice_channel_123"

	if err := queue.UpdateVoiceChannel(guildID, channelID); err != nil {
		t.Errorf("Failed to update voice channel: %v", err)
	}

	q, _ := queue.GetQueue(guildID, false)
	if q.VoiceChannelID != channelID {
		t.Errorf("Expected voice channel %s, got %s", channelID, q.VoiceChannelID)
	}
}
