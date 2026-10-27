package queue_test

import (
	"testing"

	"noraegaori/internal/config"
	"noraegaori/internal/database"
	"noraegaori/internal/queue"
)

type fieldCheck struct {
	field string
	got   any
	want  any
}

func wantFields(t *testing.T, checks []fieldCheck) {
	t.Helper()

	for i := range checks {
		check := &checks[i]
		if check.got != check.want {
			t.Errorf("%s = %v, want %v", check.field, check.got, check.want)
		}
	}
}

func TestLoadQueueFromDBAppliesEveryDefault(t *testing.T) {
	setupTestDB(t)

	q, err := queue.HookLoadQueueFromDB("guild1")
	if err != nil {
		t.Fatalf("loadQueueFromDB returned %v, want nil", err)
	}
	if q == nil {
		t.Fatal("loadQueueFromDB returned no queue")
	}

	if q.GuildID != "guild1" {
		t.Errorf("GuildID = %q, want %q", q.GuildID, "guild1")
	}
	if q.TextChannelID != "text_channel_test" {
		t.Errorf("TextChannelID = %q, want %q", q.TextChannelID, "text_channel_test")
	}
	if q.VoiceChannelID != "voice_channel_test" {
		t.Errorf("VoiceChannelID = %q, want %q", q.VoiceChannelID, "voice_channel_test")
	}

	if q.Volume != config.DefaultVolume() {
		t.Errorf("Volume = %g, want the configured default %g", q.Volume, config.DefaultVolume())
	}
	wantFields(t, []fieldCheck{
		{"AutoMixBeats", q.AutoMixBeats, 64},
		{"FadeInDuration", q.FadeInDuration, 3.0},
		{"FadeOutDuration", q.FadeOutDuration, 3.0},
		{"CrossfadeDuration", q.CrossfadeDuration, 8.0},
		{"RepeatMode", q.RepeatMode, 0},
	})

	if len(q.AutoMixOverrides) != 0 {
		t.Errorf("AutoMixOverrides = %v, want none so every style stays auto", q.AutoMixOverrides)
	}

	flags := map[string]bool{
		"SponsorBlock":  q.SponsorBlock,
		"Normalization": q.Normalization,
		"Paused":        q.Paused,
		"Playing":       q.Playing,
		"Loading":       q.Loading,
		"FadeIn":        q.FadeIn,
		"FadeOut":       q.FadeOut,
		"AutoMix":       q.AutoMix,
		"FadeOnStop":    q.FadeOnStop,
		"Crossfade":     q.Crossfade,
		"TrimSilence":   q.TrimSilence,
	}
	for name, value := range flags {
		if value {
			t.Errorf("%s = true, want false by default", name)
		}
	}

	if !q.ShowStartedTrack {
		t.Error("ShowStartedTrack = false, want true by default")
	}
	if len(q.Songs) != 0 {
		t.Errorf("Songs has %d entries, want 0", len(q.Songs))
	}
}

func TestLoadQueueFromDBMapsEveryColumnToItsOwnField(t *testing.T) {
	setupTestDB(t)

	_, err := database.DB.Exec(`INSERT INTO guild_settings (
		guild_id, volume, repeat, sponsorblock, show_started_track, normalization,
		fadein, fadeout, automix, fade_on_stop,
		fadein_duration, fadeout_duration, automix_beats,
		crossfade, crossfade_duration, trim_silence, automix_overrides
	) VALUES (?, 42, 2, 1, 0, 1, 1, 0, 1, 1, 1.5, 2.5, 32, 1, 9.5, 1, 'eq_out=hi_fast,fx_in=phaser')`, "guild1")
	if err != nil {
		t.Fatalf("failed to seed distinct settings: %v", err)
	}

	if _, err = database.DB.Exec(`UPDATE queues SET paused = 1, playing = 1, loading = 1 WHERE guild_id = ?`, "guild1"); err != nil {
		t.Fatalf("failed to seed queue state: %v", err)
	}

	queue.InvalidateCache("guild1")

	q, err := queue.HookLoadQueueFromDB("guild1")
	if err != nil {
		t.Fatalf("loadQueueFromDB returned %v, want nil", err)
	}

	wantFields(t, []fieldCheck{
		{"Volume", q.Volume, 42.0},
		{"RepeatMode", q.RepeatMode, 2},
		{"SponsorBlock", q.SponsorBlock, true},
		{"ShowStartedTrack", q.ShowStartedTrack, false},
		{"Normalization", q.Normalization, true},
		{"FadeIn", q.FadeIn, true},
		{"FadeOut", q.FadeOut, false},
		{"AutoMix", q.AutoMix, true},
		{"FadeOnStop", q.FadeOnStop, true},
		{"FadeInDuration", q.FadeInDuration, 1.5},
		{"FadeOutDuration", q.FadeOutDuration, 2.5},
		{"AutoMixBeats", q.AutoMixBeats, 32},
		{"Crossfade", q.Crossfade, true},
		{"CrossfadeDuration", q.CrossfadeDuration, 9.5},
		{"TrimSilence", q.TrimSilence, true},
	})
	if !q.Paused || !q.Playing || !q.Loading {
		t.Errorf("Paused/Playing/Loading = %v/%v/%v, want all true", q.Paused, q.Playing, q.Loading)
	}

	if len(q.AutoMixOverrides) != 2 || q.AutoMixOverrides["eq_out"] != "hi_fast" || q.AutoMixOverrides["fx_in"] != "phaser" {
		t.Errorf("AutoMixOverrides = %v, want eq_out=hi_fast and fx_in=phaser", q.AutoMixOverrides)
	}
}

func TestLoadQueueFromDBReturnsNilForAnUnknownGuild(t *testing.T) {
	setupTestDB(t)

	q, err := queue.HookLoadQueueFromDB("no-such-guild")
	if err != nil {
		t.Fatalf("loadQueueFromDB returned %v, want nil", err)
	}
	if q != nil {
		t.Errorf("got a queue for an unknown guild: %+v", q)
	}
}

func TestLoadQueueFromDBOrdersSongsByPosition(t *testing.T) {
	setupTestDB(t)

	for i := 0; i < 4; i++ {
		song := &queue.Song{
			URL:            "https://example.invalid/" + string(rune('a'+i)),
			Title:          "Song " + string(rune('a'+i)),
			RequestedByID:  "user",
			RequestedByTag: "user#0001",
		}
		if err := queue.AddSong("guild1", song, -1); err != nil {
			t.Fatalf("failed to seed song %d: %v", i, err)
		}
	}

	queue.InvalidateCache("guild1")

	q, err := queue.HookLoadQueueFromDB("guild1")
	if err != nil {
		t.Fatalf("loadQueueFromDB returned %v, want nil", err)
	}
	if len(q.Songs) != 4 {
		t.Fatalf("got %d songs, want 4", len(q.Songs))
	}
	for i, song := range q.Songs {
		if song.QueuePosition != i {
			t.Errorf("song %d has QueuePosition %d, want %d", i, song.QueuePosition, i)
		}
	}
}
