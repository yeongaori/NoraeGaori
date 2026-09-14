package queue

import (
	"fmt"
	"math"

	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func saveGuildSetting(guildID, column string, value any) error {
	release := guild.AcquireLock(guildID)
	defer release()

	if err := guild.SaveSetting(guildID, column, value); err != nil {
		return fmt.Errorf("failed to set %s: %w", column, err)
	}

	logger.Debugf("Set %s=%v for guild: %s", column, value, guildID)
	return nil
}

func SetRepeatMode(guildID string, mode int) error {
	if mode < RepeatOff || mode > RepeatSingle {
		return fmt.Errorf("invalid repeat mode: %d", mode)
	}
	return saveGuildSetting(guildID, "repeat", mode)
}

func SetVolume(guildID string, volume float64) error {
	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		return fmt.Errorf("volume must be a valid number, got: %g", volume)
	}
	if volume < 0 || volume > 1000 {
		return fmt.Errorf("volume must be between 0 and 1000, got: %g", volume)
	}
	return saveGuildSetting(guildID, "volume", volume)
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
	return saveGuildSetting(guildID, "sponsorblock", boolToInt(enabled))
}

func SetShowStartedTrack(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "show_started_track", boolToInt(enabled))
}

func SetNormalization(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "normalization", boolToInt(enabled))
}

func boolToInt(enabled bool) int {
	if enabled {
		return 1
	}
	return 0
}
