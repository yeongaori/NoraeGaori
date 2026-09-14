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
	return readSetting(guildID, "automix_beats", 16, func(settings *guildSettingsRow) int { return settings.autoMixBeats })
}

func SetAutoMixBeats(guildID string, beats int) error {
	return saveGuildSetting(guildID, "automix_beats", beats)
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
	return saveGuildSetting(guildID, column, style)
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
