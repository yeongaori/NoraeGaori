package commandtest

import (
	"testing"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func AutoLeave(isEnabled bool) (func(t *testing.T), func(t *testing.T, reply map[string]any)) {
	var before *player.GuildPlayer

	prepare := func(t *testing.T) {
		t.Helper()
		if err := queue.SetAutoLeave(GuildID, isEnabled); err != nil {
			t.Fatalf("failed to set auto-leave: %v", err)
		}
		before = player.GetPlayer(GuildID)
		t.Cleanup(func() { player.DeletePlayer(GuildID) })
	}

	check := func(t *testing.T, _ map[string]any) {
		t.Helper()
		if isKept := player.GetPlayer(GuildID) == before; isKept == isEnabled {
			t.Errorf("the player was kept = %v with auto-leave %v, want %v", isKept, isEnabled, !isEnabled)
		}
	}

	return prepare, check
}
