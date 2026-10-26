package queue

import (
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func SaveSeekTime(guildID string, songID int, seekTime int) (int, error) {
	release := guild.AcquireLock(guildID)
	defer release()

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
	release := guild.AcquireLock(guildID)
	defer release()

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
	return setQueueFlag(guildID, "paused", paused)
}

func SetPlaying(guildID string, playing bool) error {
	return setQueueFlag(guildID, "playing", playing)
}

func SetLoading(guildID string, loading bool) error {
	return setQueueFlag(guildID, "loading", loading)
}

func setQueueFlag(guildID, column string, value bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		fmt.Sprintf(`UPDATE queues SET %s = ? WHERE guild_id = ?`, column),
		boolToInt(value), guildID)
	if err != nil {
		return fmt.Errorf("failed to set %s state: %w", column, err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set %s=%v for guild: %s", column, value, guildID)
	return nil
}

func init() {
	guild.OnSettingsChange(InvalidateCache)
}

func InvalidateCache(guildID string) {
	cacheMux.Lock()
	defer cacheMux.Unlock()
	delete(cache, guildID)
	delete(settingsCache, guildID)
	settingsGenerations[guildID]++
	logger.Debugf("Invalidated cache for guild: %s", guildID)
}
