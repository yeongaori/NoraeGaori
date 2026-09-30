package player_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

func isAwaitingDiscord(guildID string) bool {
	player.HookAwaitingDiscordMu.Lock()
	defer player.HookAwaitingDiscordMu.Unlock()
	_, found := (*player.HookAwaitingDiscord)[guildID]
	return found
}

func recordOutageResumes(t *testing.T) *[]string {
	t.Helper()

	resumed := []string{}
	testutil.Swap(t, player.HookResumeAfterOutage, func(_ *discordgo.Session, guildID string) {
		resumed = append(resumed, guildID)
	})
	return &resumed
}

func joinFailsWith(t *testing.T, guildID string, err error) player.HookPlayResult {
	t.Helper()

	guildPlayer := preparedPlayer(t, guildID, 1)
	guildPlayer.HookMu().Lock()
	guildPlayer.VoiceConn = nil
	guildPlayer.HookMu().Unlock()
	testutil.Swap(t, player.HookVoiceRejoinDelay, time.Millisecond)
	stubJoinVoice(t, nil, err)

	return player.HookPlaySingleSong(nil, guildID)
}

func TestUnreachableVoiceWaitsForDiscord(t *testing.T) {
	cases := map[string]error{
		"join timed out":        fmt.Errorf("failed to join voice channel: %w", context.DeadlineExceeded),
		"gateway offline":       discordgo.ErrWSNotFound,
		"voice never ready":     errors.New("timeout waiting for voice"),
		"voice died on joining": errors.New("voice connection died: websocket closed"),
	}

	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			guildID := "unreachable" + name

			if got := joinFailsWith(t, guildID, err); got != player.HookPlayStop {
				t.Fatalf("got %v, want playStop until Discord is back", got)
			}
			if !isAwaitingDiscord(guildID) {
				t.Error("the guild is not waiting for Discord, want playback resumed after the reconnect")
			}
		})
	}
}

func TestOtherVoiceJoinFailuresDoNotWaitForDiscord(t *testing.T) {
	guildID := "joinrefused"

	if got := joinFailsWith(t, guildID, errors.New("missing permission to connect")); got != player.HookPlayStop {
		t.Fatalf("got %v, want playStop", got)
	}
	if isAwaitingDiscord(guildID) {
		t.Error("a permission failure was treated as a Discord outage")
	}
}

func addQueue(t *testing.T, guildID string, songs int) {
	t.Helper()

	if err := queue.CreateQueue(guildID, "text", "voice"); err != nil {
		t.Fatalf("failed to create the queue for %s: %v", guildID, err)
	}
	for i := range songs {
		song := &queue.Song{URL: fmt.Sprintf("https://youtube.com/watch?v=%s%d", guildID, i), Title: "Song", Duration: "3:00", RequestedByID: "user1"}
		if err := queue.AddSong(guildID, song, -1); err != nil {
			t.Fatalf("failed to add a song for %s: %v", guildID, err)
		}
	}
	t.Cleanup(func() { player.DeletePlayer(guildID) })
}

func TestResumeAfterReconnectResumesOnlyWaitingGuildsWithSongs(t *testing.T) {
	setupPlayerDB(t, "outagewaiting", 1)
	addQueue(t, "outagepaused", 1)
	addQueue(t, "outageempty", 0)
	addQueue(t, "outageunmarked", 1)
	resumed := recordOutageResumes(t)

	paused := player.GetPlayer("outagepaused")
	paused.HookMu().Lock()
	paused.Paused = true
	paused.HookMu().Unlock()
	for _, guildID := range []string{"outagewaiting", "outagepaused", "outageempty"} {
		player.HookMarkAwaitingDiscord(guildID)
	}

	player.ResumeAfterReconnect(nil)

	if !slices.Equal(*resumed, []string{"outagewaiting"}) {
		t.Errorf("resumed %v, want only the waiting guild that still has songs and is not paused", *resumed)
	}

	player.ResumeAfterReconnect(nil)
	if len(*resumed) != 1 {
		t.Errorf("resumed %v after a second reconnect, want each outage resumed once", *resumed)
	}
}

func TestWaitingForDiscordIsForgotten(t *testing.T) {
	guildID := "outageforgotten"
	guildPlayer := preparedPlayer(t, guildID, 1)

	player.HookMarkAwaitingDiscord(guildID)
	if !player.HookMarkPlayerLoading(guildPlayer, guildID) {
		t.Fatal("markPlayerLoading refused a live player")
	}
	if isAwaitingDiscord(guildID) {
		t.Error("the guild still waits for Discord after playback started")
	}

	player.HookMarkAwaitingDiscord(guildID)
	if err := player.HookReleasePlayback(guildID, false); err != nil {
		t.Fatalf("releasePlayback returned %v", err)
	}
	if isAwaitingDiscord(guildID) {
		t.Error("the guild still waits for Discord after playback was stopped")
	}

	player.HookMarkAwaitingDiscord(guildID)
	player.DeletePlayer(guildID)
	if isAwaitingDiscord(guildID) {
		t.Error("the guild still waits for Discord after its player was removed")
	}
}

func TestVoiceDropsDoNotUseUpTheSongsRetries(t *testing.T) {
	guildID := "voicedropretries"
	setupPlayerDB(t, guildID, 1)
	q, err := queue.GetQueue(guildID, true)
	if err != nil || len(q.Songs) != 1 {
		t.Fatalf("failed to load the seeded song: %v", err)
	}
	song := q.Songs[0]
	t.Cleanup(func() { player.HookClearRetryCount(guildID, song.URL) })

	for attempt := 1; attempt <= player.HookMaxRetries+2; attempt++ {
		if !player.HookHandlePlaybackError(nil, guildID, song, errors.New("voice connection died: websocket closed")) {
			t.Fatalf("attempt %d: got false, want a dropped voice connection retried", attempt)
		}
	}
	if song.GetState() == queue.SongStateFailed {
		t.Error("the song was marked failed because the voice connection dropped")
	}

	for attempt := 1; attempt < player.HookMaxRetries; attempt++ {
		if !player.HookHandlePlaybackError(nil, guildID, song, errors.New("stream stalled")) {
			t.Fatalf("attempt %d: got false, want a normal error retried until the limit", attempt)
		}
	}
	if player.HookHandlePlaybackError(nil, guildID, song, errors.New("stream stalled")) {
		t.Error("a normal error was retried past the limit after voice drops")
	}
}
