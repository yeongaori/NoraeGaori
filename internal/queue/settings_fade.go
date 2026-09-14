package queue

import (
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
	release := guild.AcquireLock(guildID)
	defer release()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadein) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadein = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadein: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set fadein=%v for guild: %s", enabled, guildID)
	return nil
}

func GetFadeInDuration(guildID string) (float64, error) {
	return readSetting(guildID, "fadein_duration", 3, func(settings *guildSettingsRow) float64 { return settings.fadeInDuration })
}

func SetFadeInDuration(guildID string, seconds float64) error {
	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadein_duration) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadein_duration = ?`,
		guildID, seconds, seconds,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadein_duration: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set fadein_duration=%g for guild: %s", seconds, guildID)
	return nil
}

func GetFadeOut(guildID string) (bool, error) {
	return readSetting(guildID, "fadeout", false, func(settings *guildSettingsRow) bool { return settings.fadeOut })
}

func SetFadeOut(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadeout) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadeout = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadeout: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set fadeout=%v for guild: %s", enabled, guildID)
	return nil
}

func GetFadeOutDuration(guildID string) (float64, error) {
	return readSetting(guildID, "fadeout_duration", 3, func(settings *guildSettingsRow) float64 { return settings.fadeOutDuration })
}

func SetFadeOutDuration(guildID string, seconds float64) error {
	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadeout_duration) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadeout_duration = ?`,
		guildID, seconds, seconds,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadeout_duration: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set fadeout_duration=%g for guild: %s", seconds, guildID)
	return nil
}

func GetAutoMix(guildID string) (bool, error) {
	return readSetting(guildID, "automix", false, func(settings *guildSettingsRow) bool { return settings.autoMix })
}

func SetAutoMix(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, automix) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET automix = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set automix: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set automix=%v for guild: %s", enabled, guildID)
	return nil
}

func GetAutoMixBeats(guildID string) (int, error) {
	return readSetting(guildID, "automix_beats", 16, func(settings *guildSettingsRow) int { return settings.autoMixBeats })
}

func SetAutoMixBeats(guildID string, beats int) error {
	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, automix_beats) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET automix_beats = ?`,
		guildID, beats, beats,
	)
	if err != nil {
		return fmt.Errorf("failed to set automix_beats: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set automix_beats=%d for guild: %s", beats, guildID)
	return nil
}

const AutoMixStyleAuto = "auto"

var autoMixStyleColumns = map[string]string{
	"volume": "automix_style_volume",
	"eq":     "automix_style_eq",
	"filter": "automix_style_filter",
	"effect": "automix_style_effect",
	"loop":   "automix_style_loop",
}

func AutoMixStyleCategories() []string {
	return []string{"volume", "eq", "filter", "effect", "loop"}
}

func GetAutoMixStyle(guildID, category string) (string, error) {
	column, ok := autoMixStyleColumns[category]
	if !ok {
		return AutoMixStyleAuto, fmt.Errorf("unknown automix style category: %s", category)
	}

	return readSetting(guildID, column, AutoMixStyleAuto, func(settings *guildSettingsRow) string {
		return defaultAutoMixStyle(autoMixStyleOf(settings, category))
	})
}

func SetAutoMixStyle(guildID, category, style string) error {
	column, ok := autoMixStyleColumns[category]
	if !ok {
		return fmt.Errorf("unknown automix style category: %s", category)
	}

	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		fmt.Sprintf(`INSERT INTO guild_settings (guild_id, %s) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET %s = ?`, column, column),
		guildID, style, style,
	)
	if err != nil {
		return fmt.Errorf("failed to set %s: %w", column, err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set %s=%s for guild: %s", column, style, guildID)
	return nil
}

var ErrSongNotInQueue = errors.New("song is no longer in the queue")

func defaultAutoMixStyle(style string) string {
	if style == "" {
		return AutoMixStyleAuto
	}
	return style
}

func SetSongAutoMixStyle(guildID string, songID int, category, style string) error {
	column, ok := autoMixStyleColumns[category]
	if !ok {
		return fmt.Errorf("unknown automix style category: %s", category)
	}

	release := guild.AcquireLock(guildID)
	defer release()

	result, err := database.DB.Exec(
		fmt.Sprintf(`UPDATE songs SET %s = ? WHERE guild_id = ? AND id = ?`, column),
		defaultAutoMixStyle(style), guildID, songID,
	)
	if err != nil {
		return fmt.Errorf("failed to set song %s: %w", column, err)
	}

	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrSongNotInQueue
	}

	InvalidateCache(guildID)
	logger.Debugf("Set %s=%s for song %d in guild: %s", column, style, songID, guildID)
	return nil
}

func GetCrossfade(guildID string) (bool, error) {
	return readSetting(guildID, "crossfade", false, func(settings *guildSettingsRow) bool { return settings.crossfade })
}

func SetCrossfade(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, crossfade) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET crossfade = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set crossfade: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set crossfade=%v for guild: %s", enabled, guildID)
	return nil
}

func GetCrossfadeDuration(guildID string) (float64, error) {
	return readSetting(guildID, "crossfade_duration", 8, func(settings *guildSettingsRow) float64 { return settings.crossfadeDuration })
}

func SetCrossfadeDuration(guildID string, seconds float64) error {
	release := guild.AcquireLock(guildID)
	defer release()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, crossfade_duration) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET crossfade_duration = ?`,
		guildID, seconds, seconds,
	)
	if err != nil {
		return fmt.Errorf("failed to set crossfade_duration: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set crossfade_duration=%g for guild: %s", seconds, guildID)
	return nil
}

func GetFadeOnStop(guildID string) (bool, error) {
	return readSetting(guildID, "fade_on_stop", false, func(settings *guildSettingsRow) bool { return settings.fadeOnStop })
}

func SetFadeOnStop(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fade_on_stop) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fade_on_stop = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set fade_on_stop: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set fade_on_stop=%v for guild: %s", enabled, guildID)
	return nil
}

func GetTrimSilence(guildID string) (bool, error) {
	return readSetting(guildID, "trim_silence", false, func(settings *guildSettingsRow) bool { return settings.trimSilence })
}

func SetTrimSilence(guildID string, enabled bool) error {
	release := guild.AcquireLock(guildID)
	defer release()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, trim_silence) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET trim_silence = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set trim_silence: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("Set trim_silence=%v for guild: %s", enabled, guildID)
	return nil
}
