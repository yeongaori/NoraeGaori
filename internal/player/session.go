package player

import (
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
)

func registeredPlayer(guildID string) *GuildPlayer {
	playersMu.RLock()
	defer playersMu.RUnlock()
	return players[guildID]
}

func (player *GuildPlayer) signalStopLocked() {
	if player.StopChan == nil {
		return
	}

	select {
	case <-player.StopChan:
		logger.Debugf("Stop signal already pending for guild: %s", player.GuildID)
	default:
		close(player.StopChan)
		logger.Debugf("Stop signal sent for guild: %s", player.GuildID)
	}
}

func (player *GuildPlayer) haltLocked() {
	player.halted = true
	player.Playing = false
	player.Loading = false
	player.signalStopLocked()
}

func (player *GuildPlayer) isHalted() bool {
	player.mu.Lock()
	defer player.mu.Unlock()
	return player.halted
}

func (player *GuildPlayer) beginSession() chan struct{} {
	player.mu.Lock()
	defer player.mu.Unlock()

	done := make(chan struct{})
	player.halted = false
	player.sessionDone = done
	return done
}

func (player *GuildPlayer) endSession(done chan struct{}) {
	player.mu.Lock()
	isCurrent := player.sessionDone == done
	if isCurrent {
		player.sessionDone = nil
	}
	isHalted := player.halted
	isPaused := player.Paused
	player.mu.Unlock()

	if isCurrent && isHalted && registeredPlayer(player.GuildID) == player {
		persistIdleState(player.GuildID, isPaused)
	}
	close(done)
}

func (player *GuildPlayer) waitForSession(timeout time.Duration) {
	player.mu.Lock()
	done := player.sessionDone
	player.mu.Unlock()

	if done == nil {
		return
	}

	select {
	case <-done:
	case <-time.After(timeout):
		logger.Warnf("Timeout waiting for the playback session to end for guild: %s", player.GuildID)
	}
}

func (player *GuildPlayer) noteCommand(name string) {
	player.mu.Lock()
	defer player.mu.Unlock()
	player.lastCommand = name
	player.lastCommandAt = time.Now()
}

func (player *GuildPlayer) recentCommand() (string, time.Duration) {
	player.mu.Lock()
	defer player.mu.Unlock()

	if player.lastCommand == "" {
		return "", 0
	}
	return player.lastCommand, time.Since(player.lastCommandAt)
}

func persistIdleState(guildID string, isPaused bool) {
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("Failed to clear playing state: %v", err)
	}
	if err := queue.SetLoading(guildID, false); err != nil {
		logger.Errorf("Failed to clear loading state: %v", err)
	}
	if !isPaused {
		return
	}
	if err := queue.SetPaused(guildID, true); err != nil {
		logger.Errorf("Failed to set paused state: %v", err)
	}
}

func ReconcileState(guildID string) {
	release, isAcquired := playLocks.TryAcquire(guildID)
	if !isAcquired {
		return
	}
	defer release()

	isMemoryActive := false
	player := registeredPlayer(guildID)
	if player != nil {
		player.mu.Lock()
		isMemoryActive = player.Playing || player.Loading
		player.Playing = false
		player.Loading = false
		player.mu.Unlock()
	}

	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		return
	}
	if !isMemoryActive && !q.Playing && !q.Loading {
		return
	}

	logger.Warnf("Reconciled stale playback state for guild %s (memory active: %v, stored playing: %v, stored loading: %v)",
		guildID, isMemoryActive, q.Playing, q.Loading)

	isPaused := len(q.Songs) > 0
	if isPaused && player != nil {
		player.mu.Lock()
		player.Paused = true
		player.mu.Unlock()
	}
	persistIdleState(guildID, isPaused)
}
