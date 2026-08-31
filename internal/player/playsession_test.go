package player

import (
	"errors"
	"testing"
	"time"

	"noraegaori/internal/testutil"

	"github.com/bwmarrin/discordgo"
)

func waitForPlayLockRelease(t *testing.T, guildID string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for playLocks.CountKeys() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("play lock for guild %s was never released", guildID)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPlayLockTimeoutDoesNotStrandGuild(t *testing.T) {
	testutil.Swap(t, &playLockWait, 20*time.Millisecond)

	guildID := "strandguild"
	entered := make(chan struct{}, 4)
	release := make(chan struct{})

	testutil.Swap(t, &playCurrentSong, func(*discordgo.Session, string) playResult {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return playStop
	})

	if err := startPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("first session returned %v, want nil", err)
	}

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first session never started")
	}

	if err := startPlaybackSession(nil, guildID); !errors.Is(err, ErrPlaybackAlreadyActive) {
		t.Fatalf("second session error = %v, want ErrPlaybackAlreadyActive", err)
	}

	close(release)
	waitForPlayLockRelease(t, guildID)

	if err := startPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("guild was left stranded after a timed-out acquisition: %v", err)
	}
	waitForPlayLockRelease(t, guildID)
}

func TestPlaySessionsAreExclusivePerGuild(t *testing.T) {
	testutil.Swap(t, &playLockWait, time.Second)

	entered := make(chan string, 2)
	release := make(chan struct{})

	testutil.Swap(t, &playCurrentSong, func(_ *discordgo.Session, guildID string) playResult {
		entered <- guildID
		<-release
		return playStop
	})

	if err := startPlaybackSession(nil, "guildA"); err != nil {
		t.Fatalf("guildA session returned %v, want nil", err)
	}
	if err := startPlaybackSession(nil, "guildB"); err != nil {
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
	t.Cleanup(func() { DeletePlayer(guildID) })

	panicked := make(chan struct{})
	testutil.Swap(t, &playCurrentSong, func(*discordgo.Session, string) playResult {
		close(panicked)
		panic("playback exploded")
	})

	if err := startPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("session returned %v, want nil", err)
	}

	select {
	case <-panicked:
	case <-time.After(2 * time.Second):
		t.Fatal("session never started")
	}

	waitForPlayLockRelease(t, guildID)

	player := GetPlayer(guildID)
	player.mu.Lock()
	isPlaying := player.Playing
	isLoading := player.Loading
	player.mu.Unlock()

	if isPlaying || isLoading {
		t.Errorf("after a panic Playing=%v Loading=%v, want both false", isPlaying, isLoading)
	}
}

func TestPlayCommandDoesNotBlockTheProcessor(t *testing.T) {
	guildID := "busyguild"
	t.Cleanup(func() { DeletePlayer(guildID) })

	entered := make(chan struct{})
	release := make(chan struct{})

	testutil.Swap(t, &playCurrentSong, func(*discordgo.Session, string) playResult {
		close(entered)
		<-release
		return playStop
	})

	if err := Play(nil, guildID); err != nil {
		t.Fatalf("Play returned %v, want nil", err)
	}

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("playback session never started")
	}

	done := make(chan error, 1)
	if err := sendCommandToPlayer(guildID, PlayerCommand{Type: inertCommandType, GuildID: guildID, Done: done}); err != nil {
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
