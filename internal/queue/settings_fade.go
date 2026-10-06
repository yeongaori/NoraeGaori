package queue

import (
	"database/sql"
	"errors"
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

func GetFadeIn(guildID string) (bool, error) {
	return readSetting(guildID, "fadein", false, func(settings *guildSettingsRow) bool { return settings.fadeIn })
}

func SetFadeIn(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "fadein", boolToInt(enabled))
}

func GetFadeInDuration(guildID string) (float64, error) {
	return readSetting(guildID, "fadein_duration", 3, func(settings *guildSettingsRow) float64 { return settings.fadeInDuration })
}

func SetFadeInDuration(guildID string, seconds float64) error {
	return saveGuildSetting(guildID, "fadein_duration", seconds)
}

func GetFadeOut(guildID string) (bool, error) {
	return readSetting(guildID, "fadeout", false, func(settings *guildSettingsRow) bool { return settings.fadeOut })
}

func SetFadeOut(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "fadeout", boolToInt(enabled))
}

func GetFadeOutDuration(guildID string) (float64, error) {
	return readSetting(guildID, "fadeout_duration", 3, func(settings *guildSettingsRow) float64 { return settings.fadeOutDuration })
}

func SetFadeOutDuration(guildID string, seconds float64) error {
	return saveGuildSetting(guildID, "fadeout_duration", seconds)
}

func GetAutoMix(guildID string) (bool, error) {
	return readSetting(guildID, "automix", false, func(settings *guildSettingsRow) bool { return settings.autoMix })
}

func SetAutoMix(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "automix", boolToInt(enabled))
}

func GetAutoMixBeats(guildID string) (int, error) {
	return readSetting(guildID, "automix_beats", 64, func(settings *guildSettingsRow) int { return settings.autoMixBeats })
}

func SetAutoMixBeats(guildID string, beats int) error {
	return saveGuildSetting(guildID, "automix_beats", beats)
}

func GetAutoMixOverrides(guildID string) (map[string]string, error) {
	return readSetting(guildID, autoMixOverridesColumn, nil, func(settings *guildSettingsRow) map[string]string {
		return settings.autoMixOverrides
	})
}

func SetAutoMixOverrides(guildID string, changes map[string]string) error {
	release := guild.AcquireLock(guildID)
	defer release()

	var stored string
	err := database.DB.QueryRow(
		`SELECT COALESCE(automix_overrides, '') FROM guild_settings WHERE guild_id = ?`, guildID,
	).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read %s: %w", autoMixOverridesColumn, err)
	}

	encoded := EncodeOverrides(ApplyOverrideChanges(DecodeOverrides(stored), changes))
	if err := guild.SaveSetting(guildID, autoMixOverridesColumn, encoded); err != nil {
		return fmt.Errorf("failed to set %s: %w", autoMixOverridesColumn, err)
	}
	logger.Debugf("Set %s=%q for guild: %s", autoMixOverridesColumn, encoded, guildID)
	return nil
}

var ErrSongNotInQueue = errors.New("song is no longer in the queue")

func SetSongAutoMixOverrides(guildID string, songID int, changes map[string]string) error {
	release := guild.AcquireLock(guildID)
	defer release()

	var stored string
	err := database.DB.QueryRow(
		`SELECT COALESCE(automix_overrides, '') FROM songs WHERE guild_id = ? AND id = ?`, guildID, songID,
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSongNotInQueue
	}
	if err != nil {
		return fmt.Errorf("failed to read song %s: %w", autoMixOverridesColumn, err)
	}

	encoded := EncodeOverrides(ApplyOverrideChanges(DecodeOverrides(stored), changes))
	if _, err := database.DB.Exec(
		`UPDATE songs SET automix_overrides = ? WHERE guild_id = ? AND id = ?`, encoded, guildID, songID,
	); err != nil {
		return fmt.Errorf("failed to set song %s: %w", autoMixOverridesColumn, err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set %s=%q for song %d in guild: %s", autoMixOverridesColumn, encoded, songID, guildID)
	return nil
}

func GetCrossfade(guildID string) (bool, error) {
	return readSetting(guildID, "crossfade", false, func(settings *guildSettingsRow) bool { return settings.crossfade })
}

func SetCrossfade(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "crossfade", boolToInt(enabled))
}

func GetCrossfadeDuration(guildID string) (float64, error) {
	return readSetting(guildID, "crossfade_duration", 8, func(settings *guildSettingsRow) float64 { return settings.crossfadeDuration })
}

func SetCrossfadeDuration(guildID string, seconds float64) error {
	return saveGuildSetting(guildID, "crossfade_duration", seconds)
}

func GetFadeOnStop(guildID string) (bool, error) {
	return readSetting(guildID, "fade_on_stop", false, func(settings *guildSettingsRow) bool { return settings.fadeOnStop })
}

func SetFadeOnStop(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "fade_on_stop", boolToInt(enabled))
}

func GetTrimSilence(guildID string) (bool, error) {
	return readSetting(guildID, "trim_silence", false, func(settings *guildSettingsRow) bool { return settings.trimSilence })
}

func SetTrimSilence(guildID string, enabled bool) error {
	return saveGuildSetting(guildID, "trim_silence", boolToInt(enabled))
}
