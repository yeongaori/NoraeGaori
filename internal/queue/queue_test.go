package queue

import (
	"os"
	"testing"

	"noraegaori/internal/database"
)

// setupTestDB creates a test database
func setupTestDB(t *testing.T) {
	os.RemoveAll("data")
	if err := database.Initialize(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}
}

// teardownTestDB removes the test database
func teardownTestDB(t *testing.T) {
	database.Close()
	os.RemoveAll("data")
}

func TestAddSong(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	testCases := []struct {
		name      string
		guildID   string
		song      *Song
		position  int
		expectErr bool
	}{
		{
			name:    "Add song to end",
			guildID: "guild1",
			song: &Song{
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
			song: &Song{
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
			song: &Song{
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := AddSong(tc.guildID, tc.song, tc.position)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}

	// Verify queue order (only after all sub-tests complete)
	t.Run("Verify queue order", func(t *testing.T) {
		q, err := GetQueue("guild1", false)
		if err != nil {
			t.Fatalf("Failed to get queue: %v", err)
		}
		if len(q.Songs) != 3 {
			t.Errorf("Expected 3 songs, got %d", len(q.Songs))
		}
	})
}

func TestRemoveSong(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Add test songs
	guildID := "guild1"
	for i := 0; i < 5; i++ {
		song := &Song{
			URL:            "https://youtube.com/watch?v=test" + string(rune('1'+i)),
			Title:          "Test Song " + string(rune('1'+i)),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
		if err := AddSong(guildID, song, -1); err != nil {
			t.Fatalf("Failed to add song: %v", err)
		}
	}

	testCases := []struct {
		name      string
		position  int
		expectErr bool
	}{
		{"Remove valid position", 2, false},
		{"Remove negative position", -1, true},
		{"Remove out of bounds", 100, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := RemoveSong(guildID, tc.position)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestSwapSongs(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	songs := []string{"Song A", "Song B", "Song C", "Song D"}
	for _, title := range songs {
		song := &Song{
			URL:            "https://youtube.com/watch?v=" + title,
			Title:          title,
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
		if err := AddSong(guildID, song, -1); err != nil {
			t.Fatalf("Failed to add song: %v", err)
		}
	}

	testCases := []struct {
		name      string
		pos1      int
		pos2      int
		expectErr bool
	}{
		{"Swap valid positions", 0, 2, false},
		{"Swap same position", 1, 1, true},
		{"Swap out of bounds", 0, 100, true},
		{"Swap negative position", -1, 1, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := SwapSongs(guildID, tc.pos1, tc.pos2)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestMoveSong(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	for i := 0; i < 5; i++ {
		song := &Song{
			URL:            "https://youtube.com/watch?v=test",
			Title:          "Song " + string(rune('A'+i)),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
		if err := AddSong(guildID, song, -1); err != nil {
			t.Fatalf("Failed to add song: %v", err)
		}
	}

	testCases := []struct {
		name      string
		from      int
		to        int
		expectErr bool
	}{
		{"Move down", 0, 3, false},
		{"Move up", 4, 1, false},
		{"Move same position", 2, 2, true},
		{"Move out of bounds", 0, 100, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := MoveSong(guildID, tc.from, tc.to)
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
	defer teardownTestDB(t)

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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := SetVolume(guildID, tc.volume)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectErr {
				q, _ := GetQueue(guildID, false)
				if q.Volume != tc.volume {
					t.Errorf("Expected volume %.0f, got %.0f", tc.volume, q.Volume)
				}
			}
		})
	}
}

func TestSetRepeatMode(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	// Test RepeatAll
	if err := SetRepeatMode(guildID, RepeatAll); err != nil {
		t.Errorf("Failed to set RepeatAll: %v", err)
	}
	q, _ := GetQueue(guildID, false)
	if q.RepeatMode != RepeatAll {
		t.Errorf("Expected RepeatAll (%d), got %d", RepeatAll, q.RepeatMode)
	}

	// Test RepeatSingle
	if err := SetRepeatMode(guildID, RepeatSingle); err != nil {
		t.Errorf("Failed to set RepeatSingle: %v", err)
	}
	q, _ = GetQueue(guildID, false)
	if q.RepeatMode != RepeatSingle {
		t.Errorf("Expected RepeatSingle (%d), got %d", RepeatSingle, q.RepeatMode)
	}

	// Test RepeatOff
	if err := SetRepeatMode(guildID, RepeatOff); err != nil {
		t.Errorf("Failed to set RepeatOff: %v", err)
	}
	q, _ = GetQueue(guildID, false)
	if q.RepeatMode != RepeatOff {
		t.Errorf("Expected RepeatOff (%d), got %d", RepeatOff, q.RepeatMode)
	}
}

func TestSetSponsorBlock(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	// Test enabling SponsorBlock
	if err := SetSponsorBlock(guildID, true); err != nil {
		t.Errorf("Failed to enable SponsorBlock: %v", err)
	}

	q, _ := GetQueue(guildID, false)
	if !q.SponsorBlock {
		t.Error("SponsorBlock should be enabled")
	}

	// Test disabling SponsorBlock
	if err := SetSponsorBlock(guildID, false); err != nil {
		t.Errorf("Failed to disable SponsorBlock: %v", err)
	}

	q, _ = GetQueue(guildID, false)
	if q.SponsorBlock {
		t.Error("SponsorBlock should be disabled")
	}
}

func TestSetShowStartedTrack(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	// Test enabling ShowStartedTrack
	if err := SetShowStartedTrack(guildID, true); err != nil {
		t.Errorf("Failed to enable ShowStartedTrack: %v", err)
	}

	q, _ := GetQueue(guildID, false)
	if !q.ShowStartedTrack {
		t.Error("ShowStartedTrack should be enabled")
	}

	// Test disabling ShowStartedTrack
	if err := SetShowStartedTrack(guildID, false); err != nil {
		t.Errorf("Failed to disable ShowStartedTrack: %v", err)
	}

	q, _ = GetQueue(guildID, false)
	if q.ShowStartedTrack {
		t.Error("ShowStartedTrack should be disabled")
	}
}

func TestSetNormalization(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	// Test enabling Normalization
	if err := SetNormalization(guildID, true); err != nil {
		t.Errorf("Failed to enable Normalization: %v", err)
	}

	q, _ := GetQueue(guildID, false)
	if !q.Normalization {
		t.Error("Normalization should be enabled")
	}

	// Test disabling Normalization
	if err := SetNormalization(guildID, false); err != nil {
		t.Errorf("Failed to disable Normalization: %v", err)
	}

	q, _ = GetQueue(guildID, false)
	if q.Normalization {
		t.Error("Normalization should be disabled")
	}
}

func TestCacheInvalidation(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	// Add a song to create cache
	song := &Song{
		URL:            "https://youtube.com/watch?v=test",
		Title:          "Test Song",
		Duration:       "3:00",
		RequestedByID:  "user1",
		RequestedByTag: "User#1234",
	}
	AddSong(guildID, song, -1)

	// Get queue (creates cache)
	q1, _ := GetQueue(guildID, false)

	// Invalidate cache
	InvalidateCache(guildID)

	// Get queue again (should fetch from DB)
	q2, _ := GetQueue(guildID, false)

	if len(q1.Songs) != len(q2.Songs) {
		t.Error("Cache invalidation affected queue content")
	}
}

func TestConcurrentAccess(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	done := make(chan bool)

	// Concurrent adds
	for i := 0; i < 10; i++ {
		go func(idx int) {
			song := &Song{
				URL:            "https://youtube.com/watch?v=test",
				Title:          "Concurrent Song",
				Duration:       "3:00",
				RequestedByID:  "user1",
				RequestedByTag: "User#1234",
			}
			AddSong(guildID, song, -1)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all songs were added
	q, _ := GetQueue(guildID, false)
	if len(q.Songs) != 10 {
		t.Errorf("Expected 10 songs after concurrent adds, got %d", len(q.Songs))
	}
}

func TestUpdateVoiceChannel(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	channelID := "voice_channel_123"

	// Update voice channel
	if err := UpdateVoiceChannel(guildID, channelID); err != nil {
		t.Errorf("Failed to update voice channel: %v", err)
	}

	// Verify update
	q, _ := GetQueue(guildID, false)
	if q.VoiceChannelID != channelID {
		t.Errorf("Expected voice channel %s, got %s", channelID, q.VoiceChannelID)
	}
}
