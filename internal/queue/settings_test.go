package queue

import (
	"fmt"
	"testing"
)

func TestBoolSettingsRoundTrip(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	cases := []struct {
		name  string
		set   func(string, bool) error
		get   func(string) (bool, error)
		field func(*Queue) bool
	}{
		{"SponsorBlock", SetSponsorBlock, GetSponsorBlock, func(q *Queue) bool { return q.SponsorBlock }},
		{"ShowStartedTrack", SetShowStartedTrack, GetShowStartedTrack, func(q *Queue) bool { return q.ShowStartedTrack }},
		{"Normalization", SetNormalization, GetNormalization, func(q *Queue) bool { return q.Normalization }},
		{"FadeIn", SetFadeIn, GetFadeIn, func(q *Queue) bool { return q.FadeIn }},
		{"FadeOut", SetFadeOut, GetFadeOut, func(q *Queue) bool { return q.FadeOut }},
		{"AutoMix", SetAutoMix, GetAutoMix, func(q *Queue) bool { return q.AutoMix }},
		{"FadeOnStop", SetFadeOnStop, GetFadeOnStop, func(q *Queue) bool { return q.FadeOnStop }},
		{"Crossfade", SetCrossfade, GetCrossfade, func(q *Queue) bool { return q.Crossfade }},
		{"TrimSilence", SetTrimSilence, GetTrimSilence, func(q *Queue) bool { return q.TrimSilence }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range []bool{true, false} {
				if err := tc.set(guildID, want); err != nil {
					t.Fatalf("set %v: %v", want, err)
				}
				got, err := tc.get(guildID)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if got != want {
					t.Errorf("getter: want %v, got %v", want, got)
				}
				q, err := GetQueue(guildID, true)
				if err != nil || q == nil {
					t.Fatalf("GetQueue: %v", err)
				}
				if tc.field(q) != want {
					t.Errorf("queue field: want %v, got %v", want, tc.field(q))
				}
			}
		})
	}
}

func TestFloatSettingsRoundTrip(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	cases := []struct {
		name  string
		set   func(string, float64) error
		get   func(string) (float64, error)
		field func(*Queue) float64
		value float64
	}{
		{"Volume", SetVolume, GetVolume, func(q *Queue) float64 { return q.Volume }, 75},
		{"FadeInDuration", SetFadeInDuration, GetFadeInDuration, func(q *Queue) float64 { return q.FadeInDuration }, 2.5},
		{"FadeOutDuration", SetFadeOutDuration, GetFadeOutDuration, func(q *Queue) float64 { return q.FadeOutDuration }, 3.5},
		{"CrossfadeDuration", SetCrossfadeDuration, GetCrossfadeDuration, func(q *Queue) float64 { return q.CrossfadeDuration }, 4.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.set(guildID, tc.value); err != nil {
				t.Fatalf("set: %v", err)
			}
			got, err := tc.get(guildID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got != tc.value {
				t.Errorf("getter: want %g, got %g", tc.value, got)
			}
			q, err := GetQueue(guildID, true)
			if err != nil || q == nil {
				t.Fatalf("GetQueue: %v", err)
			}
			if tc.field(q) != tc.value {
				t.Errorf("queue field: want %g, got %g", tc.value, tc.field(q))
			}
		})
	}
}

func TestVolumeValidation(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	for _, v := range []float64{-1, 1001} {
		if err := SetVolume(guildID, v); err == nil {
			t.Errorf("SetVolume(%g) should error", v)
		}
	}
	for _, v := range []float64{0, 100, 1000} {
		if err := SetVolume(guildID, v); err != nil {
			t.Errorf("SetVolume(%g) should succeed: %v", v, err)
		}
	}
}

func TestRepeatModeValidation(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	for _, m := range []int{-1, 3, 100} {
		if err := SetRepeatMode(guildID, m); err == nil {
			t.Errorf("SetRepeatMode(%d) should error", m)
		}
	}
	for _, m := range []int{RepeatOff, RepeatAll, RepeatSingle} {
		if err := SetRepeatMode(guildID, m); err != nil {
			t.Errorf("SetRepeatMode(%d) should succeed: %v", m, err)
		}
		got, err := GetRepeatMode(guildID)
		if err != nil || got != m {
			t.Errorf("GetRepeatMode: want %d got %d (err %v)", m, got, err)
		}
	}
}

func TestAutoMixBeatsRoundTrip(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	if err := SetAutoMixBeats(guildID, 8); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := GetAutoMixBeats(guildID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != 8 {
		t.Errorf("want 8, got %d", got)
	}
}

func TestStringSettingsRoundTrip(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	if err := SetGuildLanguage(guildID, "ko"); err != nil {
		t.Fatalf("SetGuildLanguage: %v", err)
	}
	if lang, err := GetGuildLanguage(guildID); err != nil || lang != "ko" {
		t.Errorf("GetGuildLanguage: want ko got %q (err %v)", lang, err)
	}

	if err := SetGuildPrefix(guildID, "?"); err != nil {
		t.Fatalf("SetGuildPrefix: %v", err)
	}
	if p, err := GetGuildPrefix(guildID); err != nil || p != "?" {
		t.Errorf("GetGuildPrefix: want ? got %q (err %v)", p, err)
	}
}

func TestStateFlagsRoundTrip(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	if err := SetPaused(guildID, true); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	if err := SetPlaying(guildID, true); err != nil {
		t.Fatalf("SetPlaying: %v", err)
	}
	if err := SetLoading(guildID, true); err != nil {
		t.Fatalf("SetLoading: %v", err)
	}

	q, err := GetQueue(guildID, true)
	if err != nil || q == nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if !q.Paused || !q.Playing || !q.Loading {
		t.Errorf("state flags: paused=%v playing=%v loading=%v, want all true", q.Paused, q.Playing, q.Loading)
	}
}

func TestAddSongsBatch(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"

	if err := AddSongsBatch(guildID, nil, -1); err != nil {
		t.Errorf("empty batch should be nil, got %v", err)
	}

	songs := make([]*Song, 3)
	for i := range songs {
		songs[i] = &Song{
			URL:            fmt.Sprintf("https://youtube.com/watch?v=batch%d", i),
			Title:          fmt.Sprintf("Batch %d", i),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
	}
	if err := AddSongsBatch(guildID, songs, -1); err != nil {
		t.Fatalf("AddSongsBatch: %v", err)
	}

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(q.Songs) != 3 {
		t.Errorf("want 3 songs, got %d", len(q.Songs))
	}
}

func TestAddSongsBatchNoQueue(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	if err := DeleteQueue("guild1"); err != nil {
		t.Fatalf("DeleteQueue: %v", err)
	}
	songs := []*Song{{URL: "https://youtube.com/watch?v=x", Title: "X", Duration: "1:00"}}
	if err := AddSongsBatch("guild1", songs, -1); err == nil {
		t.Error("AddSongsBatch on missing queue should error")
	}
}

func TestRemoveSongsByIDs(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 5)

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	remove := []int{q.Songs[1].ID, q.Songs[3].ID}
	if err := RemoveSongsByIDs(guildID, remove); err != nil {
		t.Fatalf("RemoveSongsByIDs: %v", err)
	}

	q, err = GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(q.Songs) != 3 {
		t.Errorf("want 3 remaining, got %d", len(q.Songs))
	}
	for i, s := range q.Songs {
		if s.QueuePosition != i {
			t.Errorf("position corruption at %d: %d", i, s.QueuePosition)
		}
	}
}

func TestSkipToPosition(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 5)

	if err := SkipToPosition(guildID, 2); err != nil {
		t.Fatalf("SkipToPosition: %v", err)
	}

	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if len(q.Songs) != 3 {
		t.Errorf("want 3 after skipping 2, got %d", len(q.Songs))
	}
	for i, s := range q.Songs {
		if s.QueuePosition != i {
			t.Errorf("position corruption at %d: %d", i, s.QueuePosition)
		}
	}
}

func TestSeekTimePersists(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 1)
	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	songID := q.Songs[0].ID

	if err := UpdateSongSeekTime(guildID, songID, 42000); err != nil {
		t.Fatalf("UpdateSongSeekTime: %v", err)
	}
	q, _ = GetQueue(guildID, true)
	if q.Songs[0].SeekTime != 42000 {
		t.Errorf("want seek 42000, got %d", q.Songs[0].SeekTime)
	}

	if _, err := SaveSeekTime(guildID, songID, 99000); err != nil {
		t.Fatalf("SaveSeekTime: %v", err)
	}
	q, _ = GetQueue(guildID, true)
	if q.Songs[0].SeekTime != 99000 {
		t.Errorf("want seek 99000, got %d", q.Songs[0].SeekTime)
	}
}

func TestDeleteQueueRemovesEverything(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	seedSongs(t, guildID, 3)

	if err := DeleteQueue(guildID); err != nil {
		t.Fatalf("DeleteQueue: %v", err)
	}
	q, err := GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if q != nil {
		t.Errorf("queue should be nil after delete, got %+v", q)
	}
}

func TestAddDuplicateSong(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	guildID := "guild1"
	song := &Song{
		URL:            "https://youtube.com/watch?v=dup",
		Title:          "Dup",
		Duration:       "3:00",
		RequestedByID:  "user1",
		RequestedByTag: "User#1234",
	}
	if err := AddSong(guildID, song, -1); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := AddSong(guildID, song, -1); err == nil {
		t.Error("duplicate URL add should error")
	}
}
