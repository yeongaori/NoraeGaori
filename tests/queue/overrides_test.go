package queue_test

import (
	"fmt"
	"testing"

	"noraegaori/internal/audio/transition"
	"noraegaori/internal/database"
	"noraegaori/internal/queue"
)

func TestOverridesRoundTripInAStableOrder(t *testing.T) {
	overrides := map[string]string{"fx_out": "phaser", "eq_in": "hi_fast", "length": "four_bars"}
	encoded := queue.EncodeOverrides(overrides)
	if encoded != "eq_in=hi_fast,fx_out=phaser,length=four_bars" {
		t.Errorf("encoded = %q, want the pairs sorted by category", encoded)
	}
	decoded := queue.DecodeOverrides(encoded)
	if len(decoded) != len(overrides) {
		t.Fatalf("decoded %v, want %v", decoded, overrides)
	}
	for key, value := range overrides {
		if decoded[key] != value {
			t.Errorf("decoded %s = %q, want %q", key, decoded[key], value)
		}
	}
}

func TestDecodeDropsBrokenPairs(t *testing.T) {
	decoded := queue.DecodeOverrides(",fx_out=phaser,=none,loop=,eq_in=auto,garbage,volume_out=slow")
	if len(decoded) != 2 || decoded["fx_out"] != "phaser" || decoded["volume_out"] != "slow" {
		t.Errorf("decoded %v, want only fx_out and volume_out", decoded)
	}
	if decoded := queue.DecodeOverrides(""); decoded != nil {
		t.Errorf("decoding nothing gave %v, want nil", decoded)
	}
}

func TestChangesCopyOnWrite(t *testing.T) {
	stored := map[string]string{"fx_out": "phaser", "eq_in": "hi_fast"}
	merged := queue.ApplyOverrideChanges(stored, map[string]string{"fx_out": "auto", "loop": "roll"})
	if len(stored) != 2 || stored["fx_out"] != "phaser" {
		t.Errorf("the stored map changed to %v, want it untouched for its other readers", stored)
	}
	if len(merged) != 2 || merged["eq_in"] != "hi_fast" || merged["loop"] != "roll" {
		t.Errorf("merged %v, want eq_in kept, fx_out removed and loop added", merged)
	}
}

func expandStored(category, value string) map[string]string {
	return transition.ExpandLegacy(transition.Category(category), value)
}

func addLegacyStyleColumns(t *testing.T) {
	t.Helper()
	for _, table := range []string{"guild_settings", "songs"} {
		for _, category := range []string{"volume", "eq", "filter", "effect", "loop"} {
			if _, err := database.DB.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN automix_style_%s TEXT DEFAULT 'auto'`, table, category)); err != nil {
				t.Fatalf("failed to add the legacy %s.%s column: %v", table, category, err)
			}
		}
	}
}

func TestLegacyStylesConvertOnceAndTheirColumnsGo(t *testing.T) {
	setupTestDB(t)
	addLegacyStyleColumns(t)

	if _, err := database.DB.Exec(`INSERT INTO guild_settings (guild_id, volume, automix_style_volume, automix_style_effect, automix_style_loop)
		VALUES ('guild1', 100, 'smooth', 'vinyl_stop', 'two_beats')`); err != nil {
		t.Fatalf("failed to seed legacy guild styles: %v", err)
	}
	song := &queue.Song{URL: "https://example.invalid/a", Title: "A", RequestedByID: "user", RequestedByTag: "user#0001"}
	if err := queue.AddSong("guild1", song, -1); err != nil {
		t.Fatalf("failed to add a song: %v", err)
	}
	if _, err := database.DB.Exec(`UPDATE songs SET automix_style_filter = 'noise_riser', automix_style_eq = 'auto' WHERE guild_id = 'guild1'`); err != nil {
		t.Fatalf("failed to seed legacy song styles: %v", err)
	}

	for run := 0; run < 2; run++ {
		if err := queue.ConvertLegacyStyles(expandStored); err != nil {
			t.Fatalf("conversion run %d failed: %v", run, err)
		}
	}

	queue.InvalidateCache("guild1")
	q, err := queue.GetQueue("guild1", true)
	if err != nil || q == nil || len(q.Songs) != 1 {
		t.Fatalf("GetQueue = (%v, %v), want the queue with its song", q, err)
	}
	wantGuild := map[string]string{"volume_out": "cross_shape", "volume_in": "cross_shape", "loop": "two_beats"}
	if fmt.Sprint(q.AutoMixOverrides) != fmt.Sprint(wantGuild) {
		t.Errorf("guild overrides = %v, want %v with the legacy loop winning over the vinyl stop", q.AutoMixOverrides, wantGuild)
	}
	wantSong := map[string]string{"filter_out": "none", "filter_in": "none", "fx_out": "noise"}
	if fmt.Sprint(q.Songs[0].AutoMixOverrides) != fmt.Sprint(wantSong) {
		t.Errorf("song overrides = %v, want %v", q.Songs[0].AutoMixOverrides, wantSong)
	}

	for _, table := range []string{"guild_settings", "songs"} {
		exists, err := database.ColumnExists(table, "automix_style_volume")
		if err != nil || exists {
			t.Errorf("%s still has automix_style_volume (%v), want the legacy columns dropped", table, err)
		}
	}
}

func TestFreshDatabaseNeedsNoConversion(t *testing.T) {
	setupTestDB(t)
	for _, table := range []string{"guild_settings", "songs"} {
		if exists, err := database.ColumnExists(table, "automix_overrides"); err != nil || !exists {
			t.Errorf("%s has no automix_overrides column (%v)", table, err)
		}
	}
	if err := queue.ConvertLegacyStyles(expandStored); err != nil {
		t.Errorf("converting a fresh database failed: %v", err)
	}
}
