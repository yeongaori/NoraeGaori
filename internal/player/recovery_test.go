package player

import (
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

type recoveryProbe struct {
	mu        sync.Mutex
	resumes   int
	crashes   []string
	leavings  []string
	songFails []string
}

func (probe *recoveryProbe) snapshot() recoveryProbe {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return recoveryProbe{
		resumes:   probe.resumes,
		crashes:   append([]string(nil), probe.crashes...),
		leavings:  append([]string(nil), probe.leavings...),
		songFails: append([]string(nil), probe.songFails...),
	}
}

func probeRecovery(t *testing.T) *recoveryProbe {
	t.Helper()

	probe := &recoveryProbe{}
	testutil.Swap(t, &resumePlayback, func(*discordgo.Session, string) error {
		probe.mu.Lock()
		probe.resumes++
		probe.mu.Unlock()
		return nil
	})
	testutil.Swap(t, &announcePlaybackCrash, func(_ *discordgo.Session, _ string, song *queue.Song) {
		probe.mu.Lock()
		probe.crashes = append(probe.crashes, song.Title)
		probe.mu.Unlock()
	})
	testutil.Swap(t, &announceLeaving, func(_ *discordgo.Session, _ string, reason string) {
		probe.mu.Lock()
		probe.leavings = append(probe.leavings, reason)
		probe.mu.Unlock()
	})
	testutil.Swap(t, &announceSongError, func(_ *discordgo.Session, _ string, song *queue.Song, _ string) {
		probe.mu.Lock()
		probe.songFails = append(probe.songFails, song.Title)
		probe.mu.Unlock()
	})
	return probe
}

func markStoredActive(t *testing.T, guildID string) {
	t.Helper()

	if err := queue.SetPlaying(guildID, true); err != nil {
		t.Fatalf("failed to mark the queue playing: %v", err)
	}
	if err := queue.SetLoading(guildID, true); err != nil {
		t.Fatalf("failed to mark the queue loading: %v", err)
	}
}

func runPanickingSession(t *testing.T, guildID string, beforePanic func(player *GuildPlayer)) {
	t.Helper()

	testutil.Swap(t, &playCurrentSong, func(_ *discordgo.Session, guildID string) playResult {
		player := GetPlayer(guildID)
		player.mu.Lock()
		player.Loading = true
		player.mu.Unlock()
		if beforePanic != nil {
			beforePanic(player)
		}
		panic("playback exploded")
	})

	if err := startPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("session returned %v, want nil", err)
	}
	waitForPlayLockRelease(t, guildID)
}

func wantIdleState(t *testing.T, guildID string) {
	t.Helper()

	state := readStoredState(t, guildID)
	if state.playing || state.loading || state.paused {
		t.Errorf("stored playing=%v loading=%v paused=%v after recovery, want all false", state.playing, state.loading, state.paused)
	}
	if IsPlaybackActive(guildID) {
		t.Error("the player still reports active playback after recovery")
	}
}

func TestUnexpectedPanicRetriesTheSong(t *testing.T) {
	guildID := "recover-retry"
	setupPlayerDB(t, guildID, 2)
	markStoredActive(t, guildID)
	probe := probeRecovery(t)

	runPanickingSession(t, guildID, nil)
	waitUntil(t, func() bool { return probe.snapshot().resumes == 1 }, "playback was never restarted after the panic")

	got := probe.snapshot()
	if len(got.crashes) != 1 || got.crashes[0] != "Song 0" {
		t.Errorf("crash announcements = %v, want one for Song 0", got.crashes)
	}
	wantIdleState(t, guildID)
	if state := readStoredState(t, guildID); state.songs != 2 {
		t.Errorf("the queue holds %d songs, want the crashed song kept for the retry", state.songs)
	}

	playbackRetriesMu.Lock()
	retries := playbackRetries[retryKey(guildID, "https://youtube.com/watch?v="+guildID+"0")]
	playbackRetriesMu.Unlock()
	if retries != 1 {
		t.Errorf("retry count = %d, want the panic counted once", retries)
	}
}

func TestPanicAfterExhaustedRetriesSkipsTheSong(t *testing.T) {
	guildID := "recover-exhausted"
	setupPlayerDB(t, guildID, 2)
	markStoredActive(t, guildID)
	probe := probeRecovery(t)

	playbackRetriesMu.Lock()
	playbackRetries[retryKey(guildID, "https://youtube.com/watch?v="+guildID+"0")] = maxRetries - 1
	playbackRetriesMu.Unlock()

	runPanickingSession(t, guildID, nil)
	waitUntil(t, func() bool { return probe.snapshot().resumes == 1 }, "playback never moved on to the next song")

	got := probe.snapshot()
	if len(got.crashes) != 0 {
		t.Errorf("crash announcements = %v, want none once the song is skipped", got.crashes)
	}
	if len(got.songFails) != 1 || got.songFails[0] != "Song 0" {
		t.Errorf("song failures = %v, want Song 0 reported", got.songFails)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 1 || q.Songs[0].Title != "Song 1" {
		t.Fatalf("queue = %+v (err %v), want only Song 1 left", q, err)
	}
	wantIdleState(t, guildID)
}

func TestPanicOnTheLastSongAfterExhaustedRetriesLeaves(t *testing.T) {
	guildID := "recover-last-song"
	setupPlayerDB(t, guildID, 1)
	markStoredActive(t, guildID)
	probe := probeRecovery(t)

	playbackRetriesMu.Lock()
	playbackRetries[retryKey(guildID, "https://youtube.com/watch?v="+guildID+"0")] = maxRetries - 1
	playbackRetriesMu.Unlock()

	runPanickingSession(t, guildID, nil)

	got := probe.snapshot()
	if len(got.leavings) != 1 || got.leavings[0] != "error" {
		t.Errorf("leaving announcements = %v, want one error once nothing is left to play", got.leavings)
	}
	if got.resumes != 0 {
		t.Errorf("playback restarted %d times with nothing left to play, want 0", got.resumes)
	}
}

func TestPanicWithAnEmptyQueueLeavesWithAnError(t *testing.T) {
	guildID := "recover-empty"
	setupPlayerDB(t, guildID, 0)
	probe := probeRecovery(t)

	runPanickingSession(t, guildID, nil)

	got := probe.snapshot()
	if len(got.leavings) != 1 || got.leavings[0] != "error" {
		t.Errorf("leaving announcements = %v, want one error", got.leavings)
	}
	if got.resumes != 0 {
		t.Errorf("playback restarted %d times with nothing to play, want 0", got.resumes)
	}
}

func TestQueuedCommandsAreRememberedForRecovery(t *testing.T) {
	player := newTestPlayer("recover-remember", func(PlayerCommand) error { return nil })
	defer player.stopTestProcessor()

	if err := <-sendTestCommand(player, "skip"); err != nil {
		t.Fatalf("the command returned %v, want nil", err)
	}

	command, age := player.recentCommand()
	if command != "skip" {
		t.Errorf("remembered command %q, want the queued skip", command)
	}
	if age >= recentCommandWindow {
		t.Errorf("the skip is %s old, want it inside the recovery window", age)
	}
}

func TestPanicCausedByATeardownIsLeftToThatCommand(t *testing.T) {
	cases := []struct {
		name        string
		beforePanic func(player *GuildPlayer)
		wantRetry   bool
	}{
		{
			name: "a halted session",
			beforePanic: func(player *GuildPlayer) {
				player.mu.Lock()
				player.haltLocked()
				player.mu.Unlock()
			},
		},
		{
			name:        "a skip just issued",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("skip") },
		},
		{
			name:        "a skipto just issued",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("skipto") },
		},
		{
			name:        "a leave just issued",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("leave") },
		},
		{
			name:        "a stop just issued",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("stop") },
		},
		{
			name:        "a pause just issued",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("pause") },
		},
		{
			name:        "a resume just issued",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("resume") },
			wantRetry:   true,
		},
		{
			name: "a skip issued long ago",
			beforePanic: func(player *GuildPlayer) {
				player.mu.Lock()
				player.lastCommand = "skip"
				player.lastCommandAt = time.Now().Add(-2 * recentCommandWindow)
				player.mu.Unlock()
			},
			wantRetry: true,
		},
		{
			name:        "a play command",
			beforePanic: func(player *GuildPlayer) { player.noteCommand("play") },
			wantRetry:   true,
		},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			guildID := "recover-teardown-" + string(rune('a'+index))
			setupPlayerDB(t, guildID, 2)
			markStoredActive(t, guildID)
			probe := probeRecovery(t)

			runPanickingSession(t, guildID, testCase.beforePanic)

			if testCase.wantRetry {
				waitUntil(t, func() bool { return probe.snapshot().resumes == 1 }, "the unexpected panic was not retried")
			} else {
				time.Sleep(20 * time.Millisecond)
				got := probe.snapshot()
				if got.resumes != 0 || len(got.crashes) != 0 {
					t.Errorf("resumes=%d crashes=%v, want recovery to leave the outcome to the command", got.resumes, got.crashes)
				}
			}
			wantIdleState(t, guildID)
		})
	}
}
