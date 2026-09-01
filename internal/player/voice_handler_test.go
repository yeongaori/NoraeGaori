package player

import (
	"testing"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/discordtest"
)

func playingPlayerWithVoice(t *testing.T, guildID string) (*GuildPlayer, *mockVoiceConn) {
	t.Helper()

	setupPlayerDB(t, guildID, 1)

	player := GetPlayer(guildID)
	conn := newMockVoiceConn()

	player.mu.Lock()
	player.Playing = true
	player.Paused = false
	player.VoiceConn = conn
	player.VoiceChannelID = "voice"
	player.StopChan = make(chan struct{})
	player.PlaybackStart = time.Now().Add(-2 * time.Second)
	player.mu.Unlock()

	t.Cleanup(func() { DeletePlayer(guildID) })
	return player, conn
}

func TestPauseForEmptyChannelWaitsForPlaybackBeforeDisconnecting(t *testing.T) {
	guildID := "guild-autopause-wait"
	player, conn := playingPlayerWithVoice(t, guildID)
	session := discordtest.Session(t, "bot")

	done := make(chan struct{})
	go func() {
		defer close(done)
		pauseForEmptyChannel(session, guildID, "voice")
	}()

	select {
	case <-player.StopChan:
	case <-time.After(2 * time.Second):
		t.Fatal("auto-pause did not signal the stop channel")
	}

	time.Sleep(150 * time.Millisecond)
	if player.currentVoice() == nil {
		t.Fatal("auto-pause tore down the voice connection before playback finished")
	}
	if got := conn.disconnectCount(); got != 0 {
		t.Fatalf("got %d disconnects while playback was still running, want 0", got)
	}

	player.PlaybackDone <- struct{}{}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("auto-pause did not finish after playback reported done")
	}

	if player.currentVoice() != nil {
		t.Error("auto-pause left the voice connection in place")
	}
	if got := conn.disconnectCount(); got != 1 {
		t.Errorf("got %d disconnects, want 1", got)
	}
}

func TestPauseForEmptyChannelMarksThePlayerPaused(t *testing.T) {
	guildID := "guild-autopause-state"
	player, _ := playingPlayerWithVoice(t, guildID)
	session := discordtest.Session(t, "bot")

	go func() {
		<-player.StopChan
		player.PlaybackDone <- struct{}{}
	}()
	pauseForEmptyChannel(session, guildID, "voice")

	player.mu.Lock()
	playing, paused := player.Playing, player.Paused
	player.mu.Unlock()

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
	player, conn := playingPlayerWithVoice(t, guildID)
	session := discordtest.Session(t, "bot")

	player.mu.Lock()
	player.Playing = false
	player.mu.Unlock()

	pauseForEmptyChannel(session, guildID, "voice")

	if got := conn.disconnectCount(); got != 0 {
		t.Errorf("got %d disconnects for an idle player, want 0", got)
	}
	if player.currentVoice() == nil {
		t.Error("auto-pause cleared the voice connection of an idle player")
	}
}

func TestSetVoiceReplacesAndClearsTheConnection(t *testing.T) {
	guildID := "guild-setvoice"
	setupPlayerDB(t, guildID, 0)

	player := GetPlayer(guildID)
	t.Cleanup(func() { DeletePlayer(guildID) })

	conn := newMockVoiceConn()
	player.setVoice(conn, "voice-1")

	if player.currentVoice() != conn {
		t.Fatal("setVoice did not store the connection")
	}
	player.mu.Lock()
	channelID := player.VoiceChannelID
	player.mu.Unlock()
	if channelID != "voice-1" {
		t.Errorf("got channel %q, want voice-1", channelID)
	}

	player.setVoice(nil, "")

	if player.currentVoice() != nil {
		t.Error("setVoice did not clear the connection")
	}
}
