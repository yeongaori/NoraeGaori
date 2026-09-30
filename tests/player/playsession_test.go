package player_test

import (
	"errors"
	"testing"
	"time"

	"noraegaori/tests/testutil"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/player"
)

func waitForPlayLockRelease(t *testing.T, guildID string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for player.HookPlayLocks.CountKeys() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("play lock for guild %s was never released", guildID)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPlayLockTimeoutDoesNotStrandGuild(t *testing.T) {
	testutil.Swap(t, player.HookPlayLockWait, 20*time.Millisecond)

	guildID := "strandguild"
	entered := make(chan struct{}, 4)
	release := make(chan struct{})

	testutil.Swap(t, player.HookPlayCurrentSong, func(*discordgo.Session, string) player.HookPlayResult {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return player.HookPlayStop
	})

	if err := player.HookStartPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("first session returned %v, want nil", err)
	}

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first session never started")
	}

	if err := player.HookStartPlaybackSession(nil, guildID); !errors.Is(err, player.ErrPlaybackAlreadyActive) {
		t.Fatalf("second session error = %v, want ErrPlaybackAlreadyActive", err)
	}

	close(release)
	waitForPlayLockRelease(t, guildID)

	if err := player.HookStartPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("guild was left stranded after a timed-out acquisition: %v", err)
	}
	waitForPlayLockRelease(t, guildID)
}

func TestPlaySessionsAreExclusivePerGuild(t *testing.T) {
	testutil.Swap(t, player.HookPlayLockWait, time.Second)

	entered := make(chan string, 2)
	release := make(chan struct{})

	testutil.Swap(t, player.HookPlayCurrentSong, func(_ *discordgo.Session, guildID string) player.HookPlayResult {
		entered <- guildID
		<-release
		return player.HookPlayStop
	})

	if err := player.HookStartPlaybackSession(nil, "guildA"); err != nil {
		t.Fatalf("guildA session returned %v, want nil", err)
	}
	if err := player.HookStartPlaybackSession(nil, "guildB"); err != nil {
		t.Fatalf("guildB session returned %v, want nil", err)
	}

	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("sessions for different guilds blocked each other")
		}
	}

	close(release)
	waitForPlayLockRelease(t, "guildA")
}

func TestPlaybackSessionRecoversFromPanic(t *testing.T) {
	guildID := "panicguild"
	t.Cleanup(func() { player.DeletePlayer(guildID) })

	panicked := make(chan struct{})
	testutil.Swap(t, player.HookPlayCurrentSong, func(*discordgo.Session, string) player.HookPlayResult {
		close(panicked)
		panic("playback exploded")
	})

	if err := player.HookStartPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("session returned %v, want nil", err)
	}

	select {
	case <-panicked:
	case <-time.After(2 * time.Second):
		t.Fatal("session never started")
	}

	waitForPlayLockRelease(t, guildID)

	guildPlayer := player.GetPlayer(guildID)
	guildPlayer.HookMu().Lock()
	isPlaying := guildPlayer.Playing
	isLoading := guildPlayer.Loading
	guildPlayer.HookMu().Unlock()

	if isPlaying || isLoading {
		t.Errorf("after a panic Playing=%v Loading=%v, want both false", isPlaying, isLoading)
	}
}

func TestPlayCommandDoesNotBlockTheProcessor(t *testing.T) {
	guildID := "busyguild"
	t.Cleanup(func() { player.DeletePlayer(guildID) })

	entered := make(chan struct{})
	release := make(chan struct{})

	testutil.Swap(t, player.HookPlayCurrentSong, func(*discordgo.Session, string) player.HookPlayResult {
		close(entered)
		<-release
		return player.HookPlayStop
	})

	if err := player.Play(nil, guildID); err != nil {
		t.Fatalf("Play returned %v, want nil", err)
	}

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("playback session never started")
	}

	done := make(chan error, 1)
	if err := player.HookSendCommandToPlayer(guildID, player.PlayerCommand{Type: inertCommandType, GuildID: guildID, Done: done}); err != nil {
		t.Fatalf("second command was not delivered: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("command processor was blocked by an in-flight playback session")
	}

	close(release)
	waitForPlayLockRelease(t, guildID)
}

func TestIsPlaybackActiveDoesNotCreateAPlayer(t *testing.T) {
	guildID := "unknownguild"

	player.HookPlayersMu.RLock()
	before := len(*player.HookPlayers)
	player.HookPlayersMu.RUnlock()

	if player.IsPlaybackActive(guildID) {
		t.Error("IsPlaybackActive reported an unknown guild as active")
	}

	player.HookPlayersMu.RLock()
	after := len(*player.HookPlayers)
	_, exists := (*player.HookPlayers)[guildID]
	player.HookPlayersMu.RUnlock()

	if exists || after != before {
		t.Errorf("IsPlaybackActive created a player: len went %d -> %d", before, after)
	}
}
