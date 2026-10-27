package player_test

import (
	"testing"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func containsAnnouncement(guildID string) bool {
	player.HookAnnouncedSongsMu.Lock()
	defer player.HookAnnouncedSongsMu.Unlock()
	_, exists := (*player.HookAnnouncedSongs)[guildID]
	return exists
}

func announceGuild(t *testing.T, guildID string) string {
	t.Helper()

	player.HookClearAnnounced(guildID)
	t.Cleanup(func() { player.HookClearAnnounced(guildID) })
	return guildID
}

func TestMarkAnnouncedFirst(t *testing.T) {
	guildID := announceGuild(t, "check-announce-first")

	if !player.HookMarkAnnounced(guildID, 101) {
		t.Error("the first announcement was suppressed")
	}
}

func TestMarkAnnouncedRepeat(t *testing.T) {
	guildID := announceGuild(t, "check-announce-repeat")

	player.HookMarkAnnounced(guildID, 101)
	if player.HookMarkAnnounced(guildID, 101) {
		t.Error("the same song announced twice")
	}
}

func TestMarkAnnouncedNewSong(t *testing.T) {
	guildID := announceGuild(t, "check-announce-different")

	player.HookMarkAnnounced(guildID, 101)
	if !player.HookMarkAnnounced(guildID, 102) {
		t.Error("a different song was suppressed")
	}
}

func TestMarkAnnouncedEarlierSong(t *testing.T) {
	guildID := announceGuild(t, "check-announce-return")

	player.HookMarkAnnounced(guildID, 101)
	player.HookMarkAnnounced(guildID, 102)
	if !player.HookMarkAnnounced(guildID, 101) {
		t.Error("returning to song 101 was suppressed")
	}
}

func TestClearAnnouncedReArms(t *testing.T) {
	guildID := announceGuild(t, "check-announce-clear")

	player.HookMarkAnnounced(guildID, 101)
	player.HookClearAnnounced(guildID)
	if !player.HookMarkAnnounced(guildID, 101) {
		t.Error("the song stayed suppressed after clearing")
	}
}

func TestMarkAnnouncedPerGuild(t *testing.T) {
	guildID := announceGuild(t, "check-announce-guild")
	other := announceGuild(t, "check-announce-guild-other")

	player.HookMarkAnnounced(guildID, 101)
	if !player.HookMarkAnnounced(other, 101) {
		t.Error("an independent guild was suppressed")
	}
}

func TestMarkAnnouncedOncePerSong(t *testing.T) {
	guildID := announceGuild(t, "check-announce-once")

	total := 0
	for _, announced := range []bool{
		player.HookMarkAnnounced(guildID, 200),
		player.HookMarkAnnounced(guildID, 200),
		player.HookMarkAnnounced(guildID, 200),
	} {
		if announced {
			total++
		}
	}

	if total != 1 {
		t.Errorf("announced %d times, want exactly 1", total)
	}
}

func TestClearAnnouncedRemovedSong(t *testing.T) {
	guildID := announceGuild(t, "check-announce-removed")

	if !player.HookMarkAnnounced(guildID, 300) {
		t.Fatal("the song was never announced")
	}
	player.HookClearAnnounced(guildID)

	if containsAnnouncement(guildID) {
		t.Error("announcement state survived the removal")
	}
}

func TestResolveRestartURLReuses(t *testing.T) {
	song := &queue.Song{ID: 1, URL: "https://example.invalid/watch?v=check"}

	got, err := player.HookResolveRestartURL("check-restart-guild", song, false, 96000, "https://cdn.invalid/existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://cdn.invalid/existing" {
		t.Errorf("got %q, want the existing stream URL", got)
	}
}

func TestResolveRestartURLLive(t *testing.T) {
	song := &queue.Song{ID: 2, URL: "https://example.invalid/watch?v=live", IsLive: true}

	got, err := player.HookResolveRestartURL("check-restart-guild", song, false, 96000, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty stream URL", got)
	}
}
