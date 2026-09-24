package guild_test

import (
	"sync"
	"testing"

	"noraegaori/internal/config"
	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/tests/testutil/configtest"
	"noraegaori/tests/testutil/dbtest"
)

func storedColumn(t *testing.T, guildID, column string) float64 {
	t.Helper()

	var value float64
	if err := database.DB.QueryRow(`SELECT `+column+` FROM guild_settings WHERE guild_id = ?`, guildID).Scan(&value); err != nil {
		t.Fatalf("failed to read %s: %v", column, err)
	}
	return value
}

func watchSettingsChanges(t *testing.T, guildID string) func() int {
	t.Helper()

	var mu sync.Mutex
	count := 0
	guild.OnSettingsChange(func(changed string) {
		if changed != guildID {
			return
		}
		mu.Lock()
		count++
		mu.Unlock()
	})

	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

func TestSaveSettingCreatesTheRowWithTheConfiguredDefaultVolume(t *testing.T) {
	dbtest.Setup(t)
	configtest.Setup(t)

	if err := guild.SaveSetting("new-guild", "sponsorblock", 1); err != nil {
		t.Fatalf("SaveSetting failed: %v", err)
	}

	if volume := storedColumn(t, "new-guild", "volume"); volume != config.DefaultVolume() {
		t.Errorf("volume = %g, want the configured %g", volume, config.DefaultVolume())
	}
	if sponsorblock := storedColumn(t, "new-guild", "sponsorblock"); sponsorblock != 1 {
		t.Errorf("sponsorblock = %g, want 1", sponsorblock)
	}
}

func TestSaveSettingKeepsAnExistingVolume(t *testing.T) {
	dbtest.Setup(t)

	if err := guild.SaveSetting("volume-guild", "volume", 70); err != nil {
		t.Fatalf("failed to save the volume: %v", err)
	}
	if err := guild.SaveSetting("volume-guild", "normalization", 1); err != nil {
		t.Fatalf("failed to save normalization: %v", err)
	}

	if volume := storedColumn(t, "volume-guild", "volume"); volume != 70 {
		t.Errorf("volume = %g after another setting was saved, want 70", volume)
	}
}

func TestSaveSettingReportsFailuresWithoutNotifying(t *testing.T) {
	dbtest.Setup(t)
	changes := watchSettingsChanges(t, "failing-guild")

	if err := guild.SaveSetting("failing-guild", "no_such_column", 1); err == nil {
		t.Error("saving an unknown column succeeded")
	}

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	})
	if err := guild.SaveSetting("failing-guild", "sponsorblock", 1); err == nil {
		t.Error("saving to a closed database succeeded")
	}

	if got := changes(); got != 0 {
		t.Errorf("failed saves notified %d times, want 0", got)
	}
}

func TestClearingThePrefixAndLanguageStoresNothing(t *testing.T) {
	dbtest.Setup(t)
	t.Cleanup(func() { guild.InvalidateCaches("clearing-guild") })

	if err := guild.SetPrefix("clearing-guild", "?"); err != nil {
		t.Fatalf("SetPrefix failed: %v", err)
	}
	if err := guild.SetPrefix("clearing-guild", ""); err != nil {
		t.Fatalf("clearing the prefix failed: %v", err)
	}
	if err := guild.SetLanguage("clearing-guild", ""); err != nil {
		t.Fatalf("clearing the language failed: %v", err)
	}

	guild.InvalidateCaches("clearing-guild")
	if prefix, err := guild.GetPrefix("clearing-guild"); err != nil || prefix != "" {
		t.Errorf("GetPrefix = (%q, %v) after clearing, want empty", prefix, err)
	}
	if language, err := guild.GetLanguage("clearing-guild"); err != nil || language != "" {
		t.Errorf("GetLanguage = (%q, %v) after clearing, want empty", language, err)
	}
}

func TestPrefixAndLanguageReportSaveFailures(t *testing.T) {
	dbtest.Setup(t)

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	})

	if err := guild.SetPrefix("closed-guild", "?"); err == nil {
		t.Error("SetPrefix on a closed database succeeded")
	}
	if err := guild.SetLanguage("closed-guild", "ko"); err == nil {
		t.Error("SetLanguage on a closed database succeeded")
	}
}

func TestPrefixAndLanguageWritesNotifySettingsChanges(t *testing.T) {
	dbtest.Setup(t)
	changes := watchSettingsChanges(t, "notify-guild")

	if err := guild.SetPrefix("notify-guild", "?"); err != nil {
		t.Fatalf("SetPrefix failed: %v", err)
	}
	if err := guild.SetLanguage("notify-guild", "ko"); err != nil {
		t.Fatalf("SetLanguage failed: %v", err)
	}

	if got := changes(); got != 2 {
		t.Errorf("prefix and language writes notified %d times, want 2", got)
	}
}
