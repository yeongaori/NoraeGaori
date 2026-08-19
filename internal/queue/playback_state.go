package queue

import (
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/pkg/logger"
)

func SaveSeekTime(guildID string, songID int, seekTime int) (int, error) {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`UPDATE songs SET seek_time = ? WHERE id = ? AND guild_id = ?`,
		seekTime, songID, guildID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to save seek time: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Saved seek time %dms for song %d in guild: %s", seekTime, songID, guildID)
	return seekTime, nil
}

func UpdateVoiceChannel(guildID, channelID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`UPDATE queues SET voice_channel_id = ? WHERE guild_id = ?`,
		channelID, guildID)
	if err != nil {
		return fmt.Errorf("failed to update voice channel: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Updated voice channel to %s for guild: %s", channelID, guildID)
	return nil
}

func SetPaused(guildID string, paused bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	pausedInt := 0
	if paused {
		pausedInt = 1
	}

	_, err := database.DB.Exec(
		`UPDATE queues SET paused = ? WHERE guild_id = ?`,
		pausedInt, guildID)
	if err != nil {
		return fmt.Errorf("failed to set paused state: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set paused=%v for guild: %s", paused, guildID)
	return nil
}

func SetPlaying(guildID string, playing bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	playingInt := 0
	if playing {
		playingInt = 1
	}

	_, err := database.DB.Exec(
		`UPDATE queues SET playing = ? WHERE guild_id = ?`,
		playingInt, guildID)
	if err != nil {
		return fmt.Errorf("failed to set playing state: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set playing=%v for guild: %s", playing, guildID)
	return nil
}

func SetLoading(guildID string, loading bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	loadingInt := 0
	if loading {
		loadingInt = 1
	}

	_, err := database.DB.Exec(
		`UPDATE queues SET loading = ? WHERE guild_id = ?`,
		loadingInt, guildID)
	if err != nil {
		return fmt.Errorf("failed to set loading state: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set loading=%v for guild: %s", loading, guildID)
	return nil
}

func InvalidateCache(guildID string) {
	cacheMux.Lock()
	defer cacheMux.Unlock()
	delete(cache, guildID)
	logger.Debugf("Invalidated cache for guild: %s", guildID)
}
