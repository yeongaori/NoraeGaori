package player

import (
	"fmt"
	"runtime/debug"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"

	"github.com/bwmarrin/discordgo"
)

var teardownCommands = map[string]bool{
	"leave":  true,
	"stop":   true,
	"pause":  true,
	"skip":   true,
	"skipto": true,
}

func recoverPlaybackSession(session *discordgo.Session, player *GuildPlayer) {
	reason := recover()
	if reason == nil {
		return
	}

	guildID := player.GuildID
	command, age := player.recentCommand()
	logger.Errorf("Playback session panicked for guild %s (last command %q, %s ago): %v\n%s",
		guildID, command, age.Round(time.Millisecond), reason, debug.Stack())

	defer func() {
		if failure := recover(); failure != nil {
			logger.Errorf("Playback recovery failed for guild %s: %v\n%s", guildID, failure, debug.Stack())
		}
	}()

	resetPlaybackState(player)

	if wasInterrupted(player, command, age) {
		logger.Infof("Playback panic in guild %s followed %q, leaving the outcome to that command", guildID, command)
		return
	}
	retryAfterPanic(session, guildID, reason)
}

func wasInterrupted(player *GuildPlayer, command string, age time.Duration) bool {
	return player.isHalted() || (teardownCommands[command] && age < recentCommandWindow)
}

func resetPlaybackState(player *GuildPlayer) {
	player.mu.Lock()
	player.Playing = false
	player.Loading = false
	player.Seeking = false
	player.TogglingNorm = false
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		pending.Stream.Stop()
	}
	persistIdleState(player.GuildID, false)
}

func retryAfterPanic(session *discordgo.Session, guildID string, reason any) {
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		leaveAfterPanic(session, guildID)
		return
	}

	song := q.Songs[0]
	if handlePlaybackError(session, guildID, song, fmt.Errorf("playback panic: %v", reason)) {
		startNextSongAsync(session, guildID)
		announcePlaybackCrash(session, guildID, song)
		return
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("Failed to remove the song that crashed playback: %v", err)
	}
	clearAnnounced(guildID)

	next, err := queue.GetQueue(guildID, true)
	if err != nil || next == nil || len(next.Songs) == 0 {
		leaveAfterPanic(session, guildID)
		return
	}
	startNextSongAsync(session, guildID)
}

func leaveAfterPanic(session *discordgo.Session, guildID string) {
	announceLeaving(session, guildID, "error")
	if err := stopInternal(guildID); err != nil {
		logger.Errorf("Failed to stop the player after a playback panic for guild %s: %v", guildID, err)
	}
}
