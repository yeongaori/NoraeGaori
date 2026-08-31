package queue

import (
	"database/sql"
	"fmt"
	"math"

	"noraegaori/internal/config"
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
	var volume float64
	err := database.DB.QueryRow(
		`SELECT volume FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&volume)

	if err == sql.ErrNoRows {

		cfg := config.GetConfig()
		if cfg != nil {
			return cfg.DefaultVolume, nil
		}
		return 100, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get volume: %w", err)
	}

	return volume, nil
}

func GetRepeatMode(guildID string) (int, error) {
	var repeat int
	err := database.DB.QueryRow(
		`SELECT repeat FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&repeat)

	if err == sql.ErrNoRows {
		return RepeatOff, nil
	}
	if err != nil {
		return RepeatOff, fmt.Errorf("failed to get repeat mode: %w", err)
	}

	return repeat, nil
}

func GetSponsorBlock(guildID string) (bool, error) {
	var sponsorblock int
	err := database.DB.QueryRow(
		`SELECT sponsorblock FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&sponsorblock)

	if err == sql.ErrNoRows {

		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get sponsorblock: %w", err)
	}

	return sponsorblock == 1, nil
}

func GetShowStartedTrack(guildID string) (bool, error) {
	var showStartedTrack int
	err := database.DB.QueryRow(
		`SELECT show_started_track FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&showStartedTrack)

	if err == sql.ErrNoRows {

		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get show_started_track: %w", err)
	}

	return showStartedTrack == 1, nil
}

func GetNormalization(guildID string) (bool, error) {
	var normalization int
	err := database.DB.QueryRow(
		`SELECT normalization FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&normalization)

	if err == sql.ErrNoRows {

		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get normalization: %w", err)
	}

	return normalization == 1, nil
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
