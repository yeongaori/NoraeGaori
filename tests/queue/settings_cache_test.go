package queue_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"noraegaori/internal/database"
	"noraegaori/internal/queue"
)

func TestSettingGettersServeRepeatReadsFromTheCache(t *testing.T) {
	setupTestDB(t)

	if err := queue.SetSponsorBlock("guild1", true); err != nil {
		t.Fatalf("failed to enable sponsorblock: %v", err)
	}
	if enabled, err := queue.GetSponsorBlock("guild1"); err != nil || !enabled {
		t.Fatalf("GetSponsorBlock = (%v, %v), want (true, nil)", enabled, err)
	}

	if _, err := database.DB.Exec(`UPDATE guild_settings SET sponsorblock = 0 WHERE guild_id = ?`, "guild1"); err != nil {
		t.Fatalf("failed to change the row behind the cache: %v", err)
	}

	if enabled, _ := queue.GetSponsorBlock("guild1"); !enabled {
		t.Error("a repeated read went to the database instead of the cache")
	}
}

func TestSettersRefreshTheCachedSettings(t *testing.T) {
	setupTestDB(t)

	if enabled, err := queue.GetFadeIn("guild1"); err != nil || enabled {
		t.Fatalf("GetFadeIn = (%v, %v), want (false, nil) before any write", enabled, err)
	}
	if err := queue.SetFadeIn("guild1", true); err != nil {
		t.Fatalf("failed to enable fade-in: %v", err)
	}
	if enabled, err := queue.GetFadeIn("guild1"); err != nil || !enabled {
		t.Errorf("GetFadeIn = (%v, %v) after enabling, want (true, nil)", enabled, err)
	}
}

func TestAStaleLoadIsNotCachedAfterAnInvalidation(t *testing.T) {
	setupTestDB(t)

	generation := queue.HookSettingsGeneration("guild1")
	queue.InvalidateCache("guild1")
	queue.HookStoreGuildSettings("guild1", generation, queue.HookDefaultGuildSettingsRow())

	queue.HookCacheMux.RLock()
	_, isCached := (*queue.HookSettingsCache)["guild1"]
	queue.HookCacheMux.RUnlock()
	if isCached {
		t.Error("a load that started before an invalidation was cached")
	}
}

func TestCachedSettingsFromAnotherDatabaseAreReloaded(t *testing.T) {
	setupTestDB(t)

	if err := queue.SetVolume("guild1", 42); err != nil {
		t.Fatalf("failed to set the volume: %v", err)
	}
	if volume, err := queue.GetVolume("guild1"); err != nil || volume != 42 {
		t.Fatalf("GetVolume = (%g, %v), want (42, nil)", volume, err)
	}
	if _, err := database.DB.Exec(`UPDATE guild_settings SET volume = 7 WHERE guild_id = ?`, "guild1"); err != nil {
		t.Fatalf("failed to change the row behind the cache: %v", err)
	}

	queue.HookCacheMux.Lock()
	*(*queue.HookSettingsCache)["guild1"].HookDb() = nil
	queue.HookCacheMux.Unlock()

	if volume, err := queue.GetVolume("guild1"); err != nil || volume != 7 {
		t.Errorf("GetVolume = (%g, %v), want the reloaded 7", volume, err)
	}
}

func TestGetQueueReloadsAQueueCachedFromAnotherDatabase(t *testing.T) {
	setupTestDB(t)

	first, err := queue.GetQueue("guild1", false)
	if err != nil || first == nil {
		t.Fatalf("GetQueue = (%v, %v), want a queue", first, err)
	}
	if again, _ := queue.GetQueue("guild1", false); again != first {
		t.Fatal("a repeated GetQueue did not use the cache")
	}

	queue.HookCacheMux.Lock()
	*(*queue.HookCache)["guild1"].HookDb() = nil
	queue.HookCacheMux.Unlock()

	if reloaded, _ := queue.GetQueue("guild1", false); reloaded == first {
		t.Error("a queue cached from another database was reused")
	}
}

func TestSettingGettersReadEveryDefault(t *testing.T) {
	setupTestDB(t)

	defaults := queue.HookDefaultGuildSettingsRow()
	guildID := "guild-without-settings"

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"volume", must(queue.GetVolume(guildID)), *defaults.HookVolume()},
		{"repeat", must(queue.GetRepeatMode(guildID)), *defaults.HookRepeat()},
		{"sponsorblock", must(queue.GetSponsorBlock(guildID)), *defaults.HookSponsorBlock()},
		{"show started track", must(queue.GetShowStartedTrack(guildID)), *defaults.HookShowStartedTrack()},
		{"normalization", must(queue.GetNormalization(guildID)), *defaults.HookNormalization()},
		{"fade-in", must(queue.GetFadeIn(guildID)), *defaults.HookFadeIn()},
		{"fade-in duration", must(queue.GetFadeInDuration(guildID)), *defaults.HookFadeInDuration()},
		{"fade-out", must(queue.GetFadeOut(guildID)), *defaults.HookFadeOut()},
		{"fade-out duration", must(queue.GetFadeOutDuration(guildID)), *defaults.HookFadeOutDuration()},
		{"automix", must(queue.GetAutoMix(guildID)), *defaults.HookAutoMix()},
		{"automix beats", must(queue.GetAutoMixBeats(guildID)), *defaults.HookAutoMixBeats()},
		{"crossfade", must(queue.GetCrossfade(guildID)), *defaults.HookCrossfade()},
		{"crossfade duration", must(queue.GetCrossfadeDuration(guildID)), *defaults.HookCrossfadeDuration()},
		{"fade on stop", must(queue.GetFadeOnStop(guildID)), *defaults.HookFadeOnStop()},
		{"trim silence", must(queue.GetTrimSilence(guildID)), *defaults.HookTrimSilence()},
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

	if q, err := queue.GetQueue("guild1", true); err == nil {
		t.Errorf("GetQueue = (%v, nil) without a settings table, want an error", q)
	}
}

func TestLoadQueueFromDBReportsAFailedSongsLoad(t *testing.T) {
	setupTestDB(t)

	if _, err := database.DB.Exec(`DROP TABLE songs`); err != nil {
		t.Fatalf("failed to drop songs: %v", err)
	}

	if q, err := queue.HookLoadQueueFromDB("guild1"); err == nil {
		t.Errorf("loadQueueFromDB = (%v, nil) without a songs table, want an error", q)
	}
}

func TestSettersKeepAnExistingRowsVolume(t *testing.T) {
	setupTestDB(t)

	if err := queue.SetVolume("guild1", 70); err != nil {
		t.Fatalf("failed to set the volume: %v", err)
	}
	if err := queue.SetSponsorBlock("guild1", true); err != nil {
		t.Fatalf("failed to enable sponsorblock: %v", err)
	}

	if volume, err := queue.GetVolume("guild1"); err != nil || volume != 70 {
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

	if err := queue.SetVolume("guild1", math.NaN()); err == nil {
		t.Error("SetVolume accepted NaN")
	}

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	if err := queue.SetSponsorBlock("guild1", true); err == nil || !strings.Contains(err.Error(), "failed to set sponsorblock") {
		t.Errorf("SetSponsorBlock on a closed database = %v, want a wrapped error", err)
	}
	if err := queue.SetAutoMixOverrides("guild1", map[string]string{"eq_out": "hi_fast"}); err == nil {
		t.Error("SetAutoMixOverrides on a closed database succeeded")
	}
}

func TestSetSongAutoMixOverrides(t *testing.T) {
	setupTestDB(t)

	song := &queue.Song{
		URL:            "https://youtube.com/watch?v=style",
		Title:          "Style Song",
		Duration:       "3:00",
		RequestedByID:  "user1",
		RequestedByTag: "User#0001",
	}
	if err := queue.AddSong("guild1", song, -1); err != nil {
		t.Fatalf("failed to add a song: %v", err)
	}
	q, err := queue.GetQueue("guild1", true)
	if err != nil || q == nil || len(q.Songs) != 1 {
		t.Fatalf("GetQueue = (%v, %v), want one song", q, err)
	}
	songID := q.Songs[0].ID

	if err := queue.SetSongAutoMixOverrides("guild1", songID, map[string]string{"eq_out": "hi_fast", "fx_in": "phaser"}); err != nil {
		t.Errorf("setting the song's styles failed: %v", err)
	}
	if err := queue.SetSongAutoMixOverrides("guild1", songID, map[string]string{"fx_in": "auto", "length": "four_bars"}); err != nil {
		t.Errorf("changing the song's styles failed: %v", err)
	}
	q, err = queue.GetQueue("guild1", true)
	if err != nil || q == nil {
		t.Fatalf("GetQueue = (%v, %v), want the queue", q, err)
	}
	if got := q.Songs[0].AutoMixOverrides; len(got) != 2 || got["eq_out"] != "hi_fast" || got["length"] != "four_bars" {
		t.Errorf("song overrides = %v, want eq_out kept, fx_in reset to auto and length added", got)
	}
	if err := queue.SetSongAutoMixOverrides("guild1", songID+1000, map[string]string{"eq_out": "hi_fast"}); !errors.Is(err, queue.ErrSongNotInQueue) {
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
	if err := queue.SetSongAutoMixOverrides("guild1", songID, map[string]string{"eq_out": "hi_fast"}); err == nil {
		t.Error("setting a song style on a closed database succeeded")
	}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func TestGuildOverridesMergeAndReset(t *testing.T) {
	setupTestDB(t)

	if err := queue.SetAutoMixOverrides("guild1", map[string]string{"volume_out": "slow", "volume_in": "slow"}); err != nil {
		t.Fatalf("failed to set the volume styles: %v", err)
	}
	if err := queue.SetAutoMixOverrides("guild1", map[string]string{"volume_in": "auto", "fx_out": "phaser"}); err != nil {
		t.Fatalf("failed to change the styles: %v", err)
	}
	got, err := queue.GetAutoMixOverrides("guild1")
	if err != nil || len(got) != 2 || got["volume_out"] != "slow" || got["fx_out"] != "phaser" {
		t.Errorf("GetAutoMixOverrides = (%v, %v), want volume_out and fx_out with volume_in back on auto", got, err)
	}
}

func TestGuildOverridesAreSharedNotCopied(t *testing.T) {
	setupTestDB(t)
	if err := queue.SetAutoMixOverrides("guild1", map[string]string{"fx_out": "phaser"}); err != nil {
		t.Fatalf("failed to set a style: %v", err)
	}
	first := must(queue.GetAutoMixOverrides("guild1"))

	allocations := testing.AllocsPerRun(50, func() {
		_ = must(queue.GetAutoMixOverrides("guild1"))
	})
	if allocations != 0 {
		t.Errorf("reading the cached overrides allocates %.0f times, want the cached map handed out", allocations)
	}
	second := must(queue.GetAutoMixOverrides("guild1"))
	if fmt.Sprintf("%p", first) != fmt.Sprintf("%p", second) {
		t.Error("two reads returned different maps, want the one decoded map shared")
	}
}

func TestSettingGettersReturnTheirFallbackWhenTheDatabaseFails(t *testing.T) {
	setupTestDB(t)
	defer func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	}()

	queue.InvalidateCache("guild1")
	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}

	if volume, err := queue.GetVolume("guild1"); err == nil || volume != 0 || !strings.Contains(err.Error(), "failed to get volume") {
		t.Errorf("GetVolume = (%g, %v), want 0 and a wrapped error", volume, err)
	}
	if beats, err := queue.GetAutoMixBeats("guild1"); err == nil || beats != 64 {
		t.Errorf("GetAutoMixBeats = (%d, %v), want 64 and an error", beats, err)
	}
	if overrides, err := queue.GetAutoMixOverrides("guild1"); err == nil || overrides != nil {
		t.Errorf("GetAutoMixOverrides = (%v, %v), want none and an error", overrides, err)
	}
}
