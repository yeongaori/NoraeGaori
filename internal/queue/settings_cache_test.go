package queue

import (
	"errors"
	"math"
	"strings"
	"testing"

	"noraegaori/internal/database"
)

func TestSettingGettersServeRepeatReadsFromTheCache(t *testing.T) {
	setupTestDB(t)

	if err := SetSponsorBlock("guild1", true); err != nil {
		t.Fatalf("failed to enable sponsorblock: %v", err)
	}
	if enabled, err := GetSponsorBlock("guild1"); err != nil || !enabled {
		t.Fatalf("GetSponsorBlock = (%v, %v), want (true, nil)", enabled, err)
	}

	if _, err := database.DB.Exec(`UPDATE guild_settings SET sponsorblock = 0 WHERE guild_id = ?`, "guild1"); err != nil {
		t.Fatalf("failed to change the row behind the cache: %v", err)
	}

	if enabled, _ := GetSponsorBlock("guild1"); !enabled {
		t.Error("a repeated read went to the database instead of the cache")
	}
}

func TestSettersRefreshTheCachedSettings(t *testing.T) {
	setupTestDB(t)

	if enabled, err := GetFadeIn("guild1"); err != nil || enabled {
		t.Fatalf("GetFadeIn = (%v, %v), want (false, nil) before any write", enabled, err)
	}
	if err := SetFadeIn("guild1", true); err != nil {
		t.Fatalf("failed to enable fade-in: %v", err)
	}
	if enabled, err := GetFadeIn("guild1"); err != nil || !enabled {
		t.Errorf("GetFadeIn = (%v, %v) after enabling, want (true, nil)", enabled, err)
	}
}

func TestAStaleLoadIsNotCachedAfterAnInvalidation(t *testing.T) {
	setupTestDB(t)

	generation := settingsGeneration("guild1")
	InvalidateCache("guild1")
	storeGuildSettings("guild1", generation, defaultGuildSettingsRow())

	cacheMux.RLock()
	_, isCached := settingsCache["guild1"]
	cacheMux.RUnlock()
	if isCached {
		t.Error("a load that started before an invalidation was cached")
	}
}

func TestCachedSettingsFromAnotherDatabaseAreReloaded(t *testing.T) {
	setupTestDB(t)

	if err := SetVolume("guild1", 42); err != nil {
		t.Fatalf("failed to set the volume: %v", err)
	}
	if volume, err := GetVolume("guild1"); err != nil || volume != 42 {
		t.Fatalf("GetVolume = (%g, %v), want (42, nil)", volume, err)
	}
	if _, err := database.DB.Exec(`UPDATE guild_settings SET volume = 7 WHERE guild_id = ?`, "guild1"); err != nil {
		t.Fatalf("failed to change the row behind the cache: %v", err)
	}

	cacheMux.Lock()
	settingsCache["guild1"].db = nil
	cacheMux.Unlock()

	if volume, err := GetVolume("guild1"); err != nil || volume != 7 {
		t.Errorf("GetVolume = (%g, %v), want the reloaded 7", volume, err)
	}
}

func TestGetQueueReloadsAQueueCachedFromAnotherDatabase(t *testing.T) {
	setupTestDB(t)

	first, err := GetQueue("guild1", false)
	if err != nil || first == nil {
		t.Fatalf("GetQueue = (%v, %v), want a queue", first, err)
	}
	if again, _ := GetQueue("guild1", false); again != first {
		t.Fatal("a repeated GetQueue did not use the cache")
	}

	cacheMux.Lock()
	cache["guild1"].db = nil
	cacheMux.Unlock()

	if reloaded, _ := GetQueue("guild1", false); reloaded == first {
		t.Error("a queue cached from another database was reused")
	}
}

func TestSettingGettersReadEveryDefault(t *testing.T) {
	setupTestDB(t)

	defaults := defaultGuildSettingsRow()
	guildID := "guild-without-settings"

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"volume", must(GetVolume(guildID)), defaults.volume},
		{"repeat", must(GetRepeatMode(guildID)), defaults.repeat},
		{"sponsorblock", must(GetSponsorBlock(guildID)), defaults.sponsorBlock},
		{"show started track", must(GetShowStartedTrack(guildID)), defaults.showStartedTrack},
		{"normalization", must(GetNormalization(guildID)), defaults.normalization},
		{"fade-in", must(GetFadeIn(guildID)), defaults.fadeIn},
		{"fade-in duration", must(GetFadeInDuration(guildID)), defaults.fadeInDuration},
		{"fade-out", must(GetFadeOut(guildID)), defaults.fadeOut},
		{"fade-out duration", must(GetFadeOutDuration(guildID)), defaults.fadeOutDuration},
		{"automix", must(GetAutoMix(guildID)), defaults.autoMix},
		{"automix beats", must(GetAutoMixBeats(guildID)), defaults.autoMixBeats},
		{"crossfade", must(GetCrossfade(guildID)), defaults.crossfade},
		{"crossfade duration", must(GetCrossfadeDuration(guildID)), defaults.crossfadeDuration},
		{"fade on stop", must(GetFadeOnStop(guildID)), defaults.fadeOnStop},
		{"trim silence", must(GetTrimSilence(guildID)), defaults.trimSilence},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %v, want the default %v", check.name, check.got, check.want)
		}
	}
}

func TestGetQueueReportsAFailedSettingsLoad(t *testing.T) {
	setupTestDB(t)

	if _, err := database.DB.Exec(`DROP TABLE guild_settings`); err != nil {
		t.Fatalf("failed to drop guild_settings: %v", err)
	}

	if q, err := GetQueue("guild1", true); err == nil {
		t.Errorf("GetQueue = (%v, nil) without a settings table, want an error", q)
	}
}

func TestLoadQueueFromDBReportsAFailedSongsLoad(t *testing.T) {
	setupTestDB(t)

	if _, err := database.DB.Exec(`DROP TABLE songs`); err != nil {
		t.Fatalf("failed to drop songs: %v", err)
	}

	if q, err := loadQueueFromDB("guild1"); err == nil {
		t.Errorf("loadQueueFromDB = (%v, nil) without a songs table, want an error", q)
	}
}

func TestSettersKeepAnExistingRowsVolume(t *testing.T) {
	setupTestDB(t)

	if err := SetVolume("guild1", 70); err != nil {
		t.Fatalf("failed to set the volume: %v", err)
	}
	if err := SetSponsorBlock("guild1", true); err != nil {
		t.Fatalf("failed to enable sponsorblock: %v", err)
	}

	if volume, err := GetVolume("guild1"); err != nil || volume != 70 {
		t.Errorf("GetVolume = (%g, %v) after another setting was saved, want (70, nil)", volume, err)
	}
}

func TestSettersReportInvalidInputAndSaveFailures(t *testing.T) {
	setupTestDB(t)
	defer func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	}()

	if err := SetVolume("guild1", math.NaN()); err == nil {
		t.Error("SetVolume accepted NaN")
	}
	if err := SetAutoMixStyle("guild1", "nope", "bass"); err == nil {
		t.Error("SetAutoMixStyle accepted an unknown category")
	}

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	if err := SetSponsorBlock("guild1", true); err == nil || !strings.Contains(err.Error(), "failed to set sponsorblock") {
		t.Errorf("SetSponsorBlock on a closed database = %v, want a wrapped error", err)
	}
}

func TestSetSongAutoMixStyle(t *testing.T) {
	setupTestDB(t)

	song := &Song{
		URL:            "https://youtube.com/watch?v=style",
		Title:          "Style Song",
		Duration:       "3:00",
		RequestedByID:  "user1",
		RequestedByTag: "User#0001",
	}
	if err := AddSong("guild1", song, -1); err != nil {
		t.Fatalf("failed to add a song: %v", err)
	}
	q, err := GetQueue("guild1", true)
	if err != nil || q == nil || len(q.Songs) != 1 {
		t.Fatalf("GetQueue = (%v, %v), want one song", q, err)
	}
	songID := q.Songs[0].ID

	if err := SetSongAutoMixStyle("guild1", songID, "eq", "bass"); err != nil {
		t.Errorf("setting the song's eq style failed: %v", err)
	}
	if err := SetSongAutoMixStyle("guild1", songID, "nope", "bass"); err == nil {
		t.Error("an unknown style category was accepted")
	}
	if err := SetSongAutoMixStyle("guild1", songID+1000, "eq", "bass"); !errors.Is(err, ErrSongNotInQueue) {
		t.Errorf("a missing song = %v, want ErrSongNotInQueue", err)
	}

	defer func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	}()
	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	if err := SetSongAutoMixStyle("guild1", songID, "eq", "bass"); err == nil {
		t.Error("setting a song style on a closed database succeeded")
	}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func TestGetAutoMixStyleReadsEveryCategory(t *testing.T) {
	setupTestDB(t)

	for _, category := range AutoMixStyleCategories() {
		if err := SetAutoMixStyle("guild1", category, category+"-style"); err != nil {
			t.Fatalf("failed to set the %s style: %v", category, err)
		}
	}
	for _, category := range AutoMixStyleCategories() {
		if style, err := GetAutoMixStyle("guild1", category); err != nil || style != category+"-style" {
			t.Errorf("GetAutoMixStyle(%s) = (%q, %v), want %q", category, style, err, category+"-style")
		}
	}

	if style, err := GetAutoMixStyle("guild1", "nope"); err == nil || style != AutoMixStyleAuto {
		t.Errorf("an unknown category = (%q, %v), want auto and an error", style, err)
	}
}

func TestSettingGettersReturnTheirFallbackWhenTheDatabaseFails(t *testing.T) {
	setupTestDB(t)
	defer func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	}()

	InvalidateCache("guild1")
	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}

	if volume, err := GetVolume("guild1"); err == nil || volume != 0 || !strings.Contains(err.Error(), "failed to get volume") {
		t.Errorf("GetVolume = (%g, %v), want 0 and a wrapped error", volume, err)
	}
	if beats, err := GetAutoMixBeats("guild1"); err == nil || beats != 16 {
		t.Errorf("GetAutoMixBeats = (%d, %v), want 16 and an error", beats, err)
	}
	if style, err := GetAutoMixStyle("guild1", "eq"); err == nil || style != AutoMixStyleAuto {
		t.Errorf("GetAutoMixStyle = (%q, %v), want auto and an error", style, err)
	}
}
