package player

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

func TestReconcileClearsStaleStoredFlagsAndPauses(t *testing.T) {
	guildID := "reconcile-stale"
	setupPlayerDB(t, guildID, 1)
	markStoredActive(t, guildID)
	player := GetPlayer(guildID)

	ReconcileState(guildID)

	state := readStoredState(t, guildID)
	if state.playing || state.loading || !state.paused {
		t.Errorf("stored playing=%v loading=%v paused=%v, want false false true", state.playing, state.loading, state.paused)
	}
	player.mu.Lock()
	isPaused := player.Paused
	player.mu.Unlock()
	if !isPaused {
		t.Error("the player was not marked paused, so play would not resume the kept song")
	}
}

func TestReconcileClearsStaleMemoryFlags(t *testing.T) {
	guildID := "reconcile-memory"
	setupPlayerDB(t, guildID, 1)
	player := GetPlayer(guildID)
	player.mu.Lock()
	player.Loading = true
	player.mu.Unlock()

	ReconcileState(guildID)

	if IsPlaybackActive(guildID) {
		t.Error("the player still reports loading with no session running")
	}
	if state := readStoredState(t, guildID); !state.paused {
		t.Error("the queue was not marked paused after clearing a stale session")
	}
}

func TestReconcileDoesNotPauseAnEmptyQueue(t *testing.T) {
	guildID := "reconcile-empty"
	setupPlayerDB(t, guildID, 0)
	markStoredActive(t, guildID)

	ReconcileState(guildID)

	state := readStoredState(t, guildID)
	if state.playing || state.loading {
		t.Errorf("stored playing=%v loading=%v, want the stale flags cleared", state.playing, state.loading)
	}
	if state.paused {
		t.Error("an empty queue was marked paused")
	}
}

func TestReconcileLeavesAnIdleQueueAlone(t *testing.T) {
	guildID := "reconcile-idle"
	setupPlayerDB(t, guildID, 1)

	ReconcileState(guildID)

	if state := readStoredState(t, guildID); state.paused {
		t.Error("an idle queue was marked paused")
	}
}

func TestReconcileNeverTouchesARunningSession(t *testing.T) {
	guildID := "reconcile-running"
	setupPlayerDB(t, guildID, 1)
	markStoredActive(t, guildID)

	entered := make(chan struct{})
	release := make(chan struct{})
	testutil.Swap(t, &playCurrentSong, func(_ *discordgo.Session, guildID string) playResult {
		player := GetPlayer(guildID)
		player.mu.Lock()
		player.Loading = true
		player.mu.Unlock()
		close(entered)
		<-release
		return playStop
	})

	if err := startPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("session returned %v, want nil", err)
	}
	<-entered

	ReconcileState(guildID)

	state := readStoredState(t, guildID)
	isActive := IsPlaybackActive(guildID)
	close(release)
	waitForPlayLockRelease(t, guildID)

	if !state.loading || !state.playing || state.paused {
		t.Errorf("stored playing=%v loading=%v paused=%v during a session, want true true false", state.playing, state.loading, state.paused)
	}
	if !isActive {
		t.Error("reconcile cleared the memory flags of a running session")
	}
}

func TestEndingAHaltedSessionPersistsTheIdleState(t *testing.T) {
	guildID := "session-halted-end"
	setupPlayerDB(t, guildID, 1)
	player := GetPlayer(guildID)
	done := player.beginSession()

	player.mu.Lock()
	player.haltLocked()
	player.Paused = true
	player.mu.Unlock()
	markStoredActive(t, guildID)
	if err := queue.SetPaused(guildID, false); err != nil {
		t.Fatalf("failed to clear the paused flag: %v", err)
	}

	player.endSession(done)

	state := readStoredState(t, guildID)
	if state.playing || state.loading || !state.paused {
		t.Errorf("stored playing=%v loading=%v paused=%v, want the halted session to leave false false true", state.playing, state.loading, state.paused)
	}
}
