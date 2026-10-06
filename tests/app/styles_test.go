package app_test

import (
	"strings"
	"testing"

	"noraegaori/internal/app"
	"noraegaori/internal/database"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/dbtest"
)

func TestStartupConvertsStoredShortcutStyles(t *testing.T) {
	dbtest.Setup(t)
	if _, err := database.DB.Exec(`ALTER TABLE guild_settings ADD COLUMN automix_style_volume TEXT DEFAULT 'auto'`); err != nil {
		t.Fatalf("failed to add the legacy column: %v", err)
	}
	if _, err := database.DB.Exec(`INSERT INTO guild_settings (guild_id, volume, automix_style_volume) VALUES ('guild1', 100, 'overlap')`); err != nil {
		t.Fatalf("failed to seed a legacy style: %v", err)
	}

	if err := app.HookConvertStoredStyles(); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	queue.InvalidateCache("guild1")
	overrides, err := queue.GetAutoMixOverrides("guild1")
	if err != nil || overrides["volume_out"] != "fast_at_end" || overrides["volume_in"] != "fast_at_start" {
		t.Errorf("overrides = (%v, %v), want the overlap shortcut expanded to both sides", overrides, err)
	}
}

func TestStartupReportsAFailedConversion(t *testing.T) {
	dbtest.Setup(t)
	dbtest.WhileClosed(t, func() {
		if err := app.HookConvertStoredStyles(); err == nil || !strings.Contains(err.Error(), "AutoMix styles") {
			t.Errorf("conversion on a closed database = %v, want a wrapped error", err)
		}
	})
}
