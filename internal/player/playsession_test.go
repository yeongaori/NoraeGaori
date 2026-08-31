package player

import (
	"errors"
	"testing"
	"time"

	"noraegaori/internal/testutil"

	"github.com/bwmarrin/discordgo"
)

func TestPlayLockTimeoutDoesNotStrandGuild(t *testing.T) {
	testutil.Swap(t, &playLockWait, 20*time.Millisecond)

	guildID := "strandguild"
	entered := make(chan struct{})
	release := make(chan struct{})

	testutil.Swap(t, &playCurrentSong, func(*discordgo.Session, string) playResult {
		close(entered)
		<-release
		return playStop
	})

	go func() {
		playInternal(nil, guildID)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first session never started")
	}

	if err := playInternal(nil, guildID); !errors.Is(err, ErrPlaybackAlreadyActive) {
		t.Fatalf("second session error = %v, want ErrPlaybackAlreadyActive", err)
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for playLocks.CountKeys() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("play lock was never reaped after the session ended")
		}
		time.Sleep(time.Millisecond)
	}

	testutil.Swap(t, &playCurrentSong, func(*discordgo.Session, string) playResult {
		return playStop
	})

	if err := playInternal(nil, guildID); err != nil {
		t.Fatalf("guild was left stranded after a timed-out acquisition: %v", err)
	}
}

func TestPlaySessionsAreExclusivePerGuild(t *testing.T) {
	testutil.Swap(t, &playLockWait, time.Second)

	bothEntered := make(chan string, 2)
	release := make(chan struct{})

	testutil.Swap(t, &playCurrentSong, func(_ *discordgo.Session, guildID string) playResult {
		bothEntered <- guildID
		<-release
		return playStop
	})

	go func() { playInternal(nil, "guildA") }()
	go func() { playInternal(nil, "guildB") }()

	for index := 0; index < 2; index++ {
		select {
		case <-bothEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("sessions for different guilds blocked each other")
		}
	}

	close(release)
}
