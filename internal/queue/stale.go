package queue

import (
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/internal/logger"
)

func ClearStalePlaybackStates() error {

	_, err := database.DB.Exec(`UPDATE queues SET playing = 0, loading = 0 WHERE playing = 1 OR loading = 1`)
	if err != nil {
		return fmt.Errorf("failed to clear stale states: %w", err)
	}
	logger.Debug("Cleared stale playback states from database")
	return nil
}
