package player

import (
	"fmt"
	"sync"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/internal/worker"
	"noraegaori/pkg/logger"
)

func GetCurrentPosition(guildID string) int {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if !player.Playing {
		return 0
	}

	elapsed := time.Since(player.PlaybackStart)
	return int(elapsed.Milliseconds())
}

func FormatDuration(ms int) string {
	seconds := ms / 1000
	minutes := seconds / 60
	hours := minutes / 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds%60)
}

func StopAll() {
	playersMu.RLock()
	guildIDs := make([]string, 0, len(players))
	for guildID := range players {
		guildIDs = append(guildIDs, guildID)
	}
	playersMu.RUnlock()

	logger.Debugf("Cleaning up %d player(s)", len(guildIDs))

	var wg sync.WaitGroup
	for _, guildID := range guildIDs {
		wg.Add(1)
		go func(guildID string) {
			defer wg.Done()
			if err := cleanupForShutdown(guildID); err != nil {
				logger.Errorf("Failed to cleanup guild %s: %v", guildID, err)
			}
			ClearPreCache(guildID)
			StopAnalysisBackfill(guildID)
			logger.Debugf("Cleaned up guild: %s", guildID)
		}(guildID)
	}
	wg.Wait()

	preCacheStoreMu.Lock()
	for key, cache := range preCacheStore {
		if cache.CancelFunc != nil {
			cache.CancelFunc()
		}
		delete(preCacheStore, key)
	}
	preCacheStoreMu.Unlock()

	logger.Debug("All players cleaned up and pre-cache cleared")
}

func cleanupForShutdown(guildID string) error {
	defer callOnPlaybackEnded(guildID)

	logger.Debugf("Cleaning up guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.StopChan:

		default:
			close(player.StopChan)
			logger.Debugf("Stop signal sent for guild: %s", guildID)
		}
	}

	player.Playing = false
	player.Paused = false
	player.Loading = false
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		pending.Stream.stop()
	}

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("Playback terminated for guild: %s", guildID)
		case <-time.After(2 * time.Second):
			logger.Debugf("Timeout waiting for playback for guild: %s", guildID)
		}
	}

	if err := LeaveVoice(guildID); err != nil {
		logger.Debugf("Failed to leave voice for guild %s: %v", guildID, err)
	}

	if wasPlaying || wasLoading {
		if err := queue.SetPlaying(guildID, false); err != nil {
			logger.Debugf("Failed to clear playing state for guild %s: %v", guildID, err)
		}
		if err := queue.SetLoading(guildID, false); err != nil {
			logger.Debugf("Failed to clear loading state for guild %s: %v", guildID, err)
		}
	}

	DeletePlayer(guildID)

	logger.Debugf("Cleanup complete for guild: %s (queue preserved)", guildID)
	return nil
}

func ShutdownWorkerPool() {
	worker.CloseGlobalPool()
}
