package player_test

import (
	"testing"
	"time"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/discordtest"
)

func playingPlayerWithVoice(t *testing.T, guildID string) (*player.GuildPlayer, *mockVoiceConn, chan struct{}) {
	t.Helper()

	setupPlayerDB(t, guildID, 1)

	guildPlayer := player.GetPlayer(guildID)
	conn := newMockVoiceConn()
	sessionDone := guildPlayer.HookBeginSession()

	guildPlayer.HookMu().Lock()
	guildPlayer.Playing = true
	guildPlayer.Paused = false
	guildPlayer.VoiceConn = conn
	guildPlayer.VoiceChannelID = "voice"
	guildPlayer.StopChan = make(chan struct{})
	guildPlayer.PlaybackStart = time.Now().Add(-2 * time.Second)
	guildPlayer.HookMu().Unlock()

	t.Cleanup(func() { player.DeletePlayer(guildID) })
	return guildPlayer, conn, sessionDone
}

func TestPauseForEmptyChannelWaitsForPlaybackBeforeDisconnecting(t *testing.T) {
	guildID := "guild-autopause-wait"
	guildPlayer, conn, sessionDone := playingPlayerWithVoice(t, guildID)
	session := discordtest.Session(t, "bot")

	done := make(chan struct{})
	go func() {
		defer close(done)
		player.HookPauseForEmptyChannel(session, guildID, "voice")
	}()

	select {
	case <-guildPlayer.StopChan:
	case <-time.After(2 * time.Second):
		t.Fatal("auto-pause did not signal the stop channel")
	}

	time.Sleep(150 * time.Millisecond)
	if guildPlayer.HookCurrentVoice() == nil {
		t.Fatal("auto-pause tore down the voice connection before playback finished")
	}
	if got := conn.disconnectCount(); got != 0 {
		t.Fatalf("got %d disconnects while playback was still running, want 0", got)
	}

	guildPlayer.HookEndSession(sessionDone)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("auto-pause did not finish after the playback session ended")
	}

	if guildPlayer.HookCurrentVoice() != nil {
		t.Error("auto-pause left the voice connection in place")
	}
	if got := conn.disconnectCount(); got != 1 {
		t.Errorf("got %d disconnects, want 1", got)
	}
}

func TestPauseForEmptyChannelMarksThePlayerPaused(t *testing.T) {
	guildID := "guild-autopause-state"
	guildPlayer, _, sessionDone := playingPlayerWithVoice(t, guildID)
	session := discordtest.Session(t, "bot")

	go func() {
		<-guildPlayer.StopChan
		guildPlayer.HookEndSession(sessionDone)
	}()
	player.HookPauseForEmptyChannel(session, guildID, "voice")

	guildPlayer.HookMu().Lock()
	playing, paused := guildPlayer.Playing, guildPlayer.Paused
	guildPlayer.HookMu().Unlock()

	if playing {
		t.Error("the player is still marked as playing")
	}
	if !paused {
		t.Error("the player is not marked as paused")
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if !q.Paused {
		t.Error("the stored queue is not marked as paused")
	}
	if q.Playing {
		t.Error("the stored queue is still marked as playing")
	}
}

func TestPauseForEmptyChannelIgnoresAnIdlePlayer(t *testing.T) {
	guildID := "guild-autopause-idle"
	guildPlayer, conn, _ := playingPlayerWithVoice(t, guildID)
	session := discordtest.Session(t, "bot")

	guildPlayer.HookMu().Lock()
	guildPlayer.Playing = false
	guildPlayer.HookMu().Unlock()

	t.Cleanup(func() { player.HookForgetAutoPause(guildID) })

	player.HookPauseForEmptyChannel(session, guildID, "voice")

	if got := conn.disconnectCount(); got != 0 {
		t.Errorf("got %d disconnects for an idle player, want 0", got)
	}
	if guildPlayer.HookCurrentVoice() == nil {
		t.Error("auto-pause cleared the voice connection of an idle player")
	}
	if got := player.HookAutoPausedChannel(guildID); got != "" {
		t.Errorf("an idle player was remembered as auto-paused in %q", got)
	}
}

func TestSetVoiceReplacesAndClearsTheConnection(t *testing.T) {
	guildID := "guild-setvoice"
	setupPlayerDB(t, guildID, 0)

	guildPlayer := player.GetPlayer(guildID)
	t.Cleanup(func() { player.DeletePlayer(guildID) })

	conn := newMockVoiceConn()
	guildPlayer.HookSetVoice(conn, "voice-1")

	if guildPlayer.HookCurrentVoice() != conn {
		t.Fatal("setVoice did not store the connection")
	}
	guildPlayer.HookMu().Lock()
	channelID := guildPlayer.VoiceChannelID
	guildPlayer.HookMu().Unlock()
	if channelID != "voice-1" {
		t.Errorf("got channel %q, want voice-1", channelID)
	}

	guildPlayer.HookSetVoice(nil, "")

	if guildPlayer.HookCurrentVoice() != nil {
		t.Error("setVoice did not clear the connection")
	}
}
