package queue

import (
	"fmt"
	"math"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func SetRepeatMode(guildID string, mode int) error {
	if mode < RepeatOff || mode > RepeatSingle {
		return fmt.Errorf("invalid repeat mode: %d", mode)
	}

	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, repeat) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET repeat = ?`,
		guildID, mode, mode,
	)
	if err != nil {
		return fmt.Errorf("failed to set repeat mode: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set repeat=%d for guild: %s", mode, guildID)
	return nil
}

func SetVolume(guildID string, volume float64) error {

	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		return fmt.Errorf("volume must be a valid number, got: %g", volume)
	}

	if volume < 0 || volume > 1000 {
		return fmt.Errorf("volume must be between 0 and 1000, got: %g", volume)
	}

	release := guild.AcquireLock(guildID)
	defer release()

	result, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, volume) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET volume = ?`,
		guildID, volume, volume,
	)
	if err != nil {
		logger.Errorf("Database error for guild %s: %v", guildID, err)
		return fmt.Errorf("failed to set volume: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	logger.Debugf("Set volume=%g for guild %s (rows affected: %d)", volume, guildID, rowsAffected)

	InvalidateCache(guildID)
	logger.Debugf("Invalidated cache for guild: %s", guildID)
	return nil
}

func GetVolume(guildID string) (float64, error) {
	return readSetting(guildID, "volume", 0, func(settings *guildSettingsRow) float64 { return settings.volume })
}

func GetRepeatMode(guildID string) (int, error) {
	return readSetting(guildID, "repeat mode", RepeatOff, func(settings *guildSettingsRow) int { return settings.repeat })
}

func GetSponsorBlock(guildID string) (bool, error) {
	return readSetting(guildID, "sponsorblock", false, func(settings *guildSettingsRow) bool { return settings.sponsorBlock })
}

func GetShowStartedTrack(guildID string) (bool, error) {
	return readSetting(guildID, "show_started_track", false, func(settings *guildSettingsRow) bool { return settings.showStartedTrack })
}

func GetNormalization(guildID string) (bool, error) {
	return readSetting(guildID, "normalization", false, func(settings *guildSettingsRow) bool { return settings.normalization })
}

func SetSponsorBlock(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	sponsorblockInt := 0
	if enabled {
		sponsorblockInt = 1
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, sponsorblock) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET sponsorblock = ?`,
		guildID, sponsorblockInt, sponsorblockInt,
	)
	if err != nil {
		return fmt.Errorf("failed to set sponsorblock: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set sponsorblock=%v for guild: %s", enabled, guildID)
	return nil
}

func SetShowStartedTrack(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	showStartedTrackInt := 0
	if enabled {
		showStartedTrackInt = 1
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, show_started_track) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET show_started_track = ?`,
		guildID, showStartedTrackInt, showStartedTrackInt,
	)
	if err != nil {
		return fmt.Errorf("failed to set show_started_track: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set show_started_track=%v for guild: %s", enabled, guildID)
	return nil
}

func SetNormalization(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	normalizationInt := 0
	if enabled {
		normalizationInt = 1
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, normalization) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET normalization = ?`,
		guildID, normalizationInt, normalizationInt,
	)
	if err != nil {
		return fmt.Errorf("failed to set normalization: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set normalization=%v for guild: %s", enabled, guildID)
	return nil
}

func boolToInt(enabled bool) int {
	if enabled {
		return 1
	}
	return 0
}
