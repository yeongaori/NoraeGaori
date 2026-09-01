package queue

import (
	"testing"

	"noraegaori/internal/database"
)

func TestLoadQueueFromDBAppliesEveryDefault(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	q, err := loadQueueFromDB("guild1")
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

	if q.Volume != 100 {
		t.Errorf("Volume = %g, want 100", q.Volume)
	}
	if q.AutoMixBeats != 16 {
		t.Errorf("AutoMixBeats = %d, want 16", q.AutoMixBeats)
	}
	if q.FadeInDuration != 3 {
		t.Errorf("FadeInDuration = %g, want 3", q.FadeInDuration)
	}
	if q.FadeOutDuration != 3 {
		t.Errorf("FadeOutDuration = %g, want 3", q.FadeOutDuration)
	}
	if q.CrossfadeDuration != 8 {
		t.Errorf("CrossfadeDuration = %g, want 8", q.CrossfadeDuration)
	}

	styles := map[string]string{
		"AutoMixStyleVolume": q.AutoMixStyleVolume,
		"AutoMixStyleEQ":     q.AutoMixStyleEQ,
		"AutoMixStyleFilter": q.AutoMixStyleFilter,
		"AutoMixStyleEffect": q.AutoMixStyleEffect,
		"AutoMixStyleLoop":   q.AutoMixStyleLoop,
	}
	for name, value := range styles {
		if value != "auto" {
			t.Errorf("%s = %q, want %q", name, value, "auto")
		}
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
	if q.RepeatMode != 0 {
		t.Errorf("RepeatMode = %d, want 0", q.RepeatMode)
	}
	if len(q.Songs) != 0 {
		t.Errorf("Songs has %d entries, want 0", len(q.Songs))
	}
}

func TestLoadQueueFromDBMapsEveryColumnToItsOwnField(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	_, err := database.DB.Exec(`INSERT INTO guild_settings (
		guild_id, volume, repeat, sponsorblock, show_started_track, normalization,
		fadein, fadeout, automix, fade_on_stop,
		fadein_duration, fadeout_duration, automix_beats,
		crossfade, crossfade_duration, trim_silence,
		automix_style_volume, automix_style_eq, automix_style_filter,
		automix_style_effect, automix_style_loop
	) VALUES (?, 42, 2, 1, 0, 1, 1, 0, 1, 1, 1.5, 2.5, 32, 1, 9.5, 1, 'sv', 'se', 'sf', 'sx', 'sl')`, "guild1")
	if err != nil {
		t.Fatalf("failed to seed distinct settings: %v", err)
	}

	if _, err := database.DB.Exec(`UPDATE queues SET paused = 1, playing = 1, loading = 1 WHERE guild_id = ?`, "guild1"); err != nil {
		t.Fatalf("failed to seed queue state: %v", err)
	}

	InvalidateCache("guild1")

	q, err := loadQueueFromDB("guild1")
	if err != nil {
		t.Fatalf("loadQueueFromDB returned %v, want nil", err)
	}

	if q.Volume != 42 {
		t.Errorf("Volume = %g, want 42", q.Volume)
	}
	if q.RepeatMode != 2 {
		t.Errorf("RepeatMode = %d, want 2", q.RepeatMode)
	}
	if !q.SponsorBlock {
		t.Error("SponsorBlock = false, want true")
	}
	if q.ShowStartedTrack {
		t.Error("ShowStartedTrack = true, want false")
	}
	if !q.Normalization {
		t.Error("Normalization = false, want true")
	}
	if !q.FadeIn {
		t.Error("FadeIn = false, want true")
	}
	if q.FadeOut {
		t.Error("FadeOut = true, want false")
	}
	if !q.AutoMix {
		t.Error("AutoMix = false, want true")
	}
	if !q.FadeOnStop {
		t.Error("FadeOnStop = false, want true")
	}
	if q.FadeInDuration != 1.5 {
		t.Errorf("FadeInDuration = %g, want 1.5", q.FadeInDuration)
	}
	if q.FadeOutDuration != 2.5 {
		t.Errorf("FadeOutDuration = %g, want 2.5", q.FadeOutDuration)
	}
	if q.AutoMixBeats != 32 {
		t.Errorf("AutoMixBeats = %d, want 32", q.AutoMixBeats)
	}
	if !q.Crossfade {
		t.Error("Crossfade = false, want true")
	}
	if q.CrossfadeDuration != 9.5 {
		t.Errorf("CrossfadeDuration = %g, want 9.5", q.CrossfadeDuration)
	}
	if !q.TrimSilence {
		t.Error("TrimSilence = false, want true")
	}
	if !q.Paused || !q.Playing || !q.Loading {
		t.Errorf("Paused/Playing/Loading = %v/%v/%v, want all true", q.Paused, q.Playing, q.Loading)
	}

	styles := map[string]string{
		"AutoMixStyleVolume": q.AutoMixStyleVolume,
		"AutoMixStyleEQ":     q.AutoMixStyleEQ,
		"AutoMixStyleFilter": q.AutoMixStyleFilter,
		"AutoMixStyleEffect": q.AutoMixStyleEffect,
		"AutoMixStyleLoop":   q.AutoMixStyleLoop,
	}
	want := map[string]string{
		"AutoMixStyleVolume": "sv",
		"AutoMixStyleEQ":     "se",
		"AutoMixStyleFilter": "sf",
		"AutoMixStyleEffect": "sx",
		"AutoMixStyleLoop":   "sl",
	}
	for name, value := range styles {
		if value != want[name] {
			t.Errorf("%s = %q, want %q", name, value, want[name])
		}
	}
}

func TestLoadQueueFromDBReturnsNilForAnUnknownGuild(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	q, err := loadQueueFromDB("no-such-guild")
	if err != nil {
		t.Fatalf("loadQueueFromDB returned %v, want nil", err)
	}
	if q != nil {
		t.Errorf("got a queue for an unknown guild: %+v", q)
	}
}

func TestLoadQueueFromDBOrdersSongsByPosition(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	for i := 0; i < 4; i++ {
		song := &Song{
			URL:            "https://example.invalid/" + string(rune('a'+i)),
			Title:          "Song " + string(rune('a'+i)),
			RequestedByID:  "user",
			RequestedByTag: "user#0001",
		}
		if err := AddSong("guild1", song, -1); err != nil {
			t.Fatalf("failed to seed song %d: %v", i, err)
		}
	}

	InvalidateCache("guild1")

	q, err := loadQueueFromDB("guild1")
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
