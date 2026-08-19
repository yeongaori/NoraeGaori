package queue

import (
	"database/sql"
	"errors"
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/pkg/logger"
)

func GetFadeIn(guildID string) (bool, error) {
	var fadein int
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadein, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&fadein)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get fadein: %w", err)
	}

	return fadein == 1, nil
}

func SetFadeIn(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var duration float64
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadein_duration, 3) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&duration)

	if err == sql.ErrNoRows {
		return 3, nil
	}
	if err != nil {
		return 3, fmt.Errorf("failed to get fadein_duration: %w", err)
	}

	return duration, nil
}

func SetFadeInDuration(guildID string, seconds float64) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var fadeout int
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadeout, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&fadeout)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get fadeout: %w", err)
	}

	return fadeout == 1, nil
}

func SetFadeOut(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var duration float64
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadeout_duration, 3) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&duration)

	if err == sql.ErrNoRows {
		return 3, nil
	}
	if err != nil {
		return 3, fmt.Errorf("failed to get fadeout_duration: %w", err)
	}

	return duration, nil
}

func SetFadeOutDuration(guildID string, seconds float64) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var automix int
	err := database.DB.QueryRow(
		`SELECT COALESCE(automix, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&automix)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get automix: %w", err)
	}

	return automix == 1, nil
}

func SetAutoMix(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var beats int
	err := database.DB.QueryRow(
		`SELECT COALESCE(automix_beats, 16) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&beats)

	if err == sql.ErrNoRows {
		return 16, nil
	}
	if err != nil {
		return 16, fmt.Errorf("failed to get automix_beats: %w", err)
	}

	return beats, nil
}

func SetAutoMixBeats(guildID string, beats int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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

	var style string
	err := database.DB.QueryRow(
		fmt.Sprintf(`SELECT COALESCE(%s, '%s') FROM guild_settings WHERE guild_id = ?`, column, AutoMixStyleAuto),
		guildID,
	).Scan(&style)

	if err == sql.ErrNoRows {
		return AutoMixStyleAuto, nil
	}
	if err != nil {
		return AutoMixStyleAuto, fmt.Errorf("failed to get %s: %w", column, err)
	}

	if style == "" {
		return AutoMixStyleAuto, nil
	}
	return style, nil
}

func SetAutoMixStyle(guildID, category, style string) error {
	column, ok := autoMixStyleColumns[category]
	if !ok {
		return fmt.Errorf("unknown automix style category: %s", category)
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var crossfade int
	err := database.DB.QueryRow(
		`SELECT COALESCE(crossfade, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&crossfade)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get crossfade: %w", err)
	}

	return crossfade == 1, nil
}

func SetCrossfade(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var duration float64
	err := database.DB.QueryRow(
		`SELECT COALESCE(crossfade_duration, 8) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&duration)

	if err == sql.ErrNoRows {
		return 8, nil
	}
	if err != nil {
		return 8, fmt.Errorf("failed to get crossfade_duration: %w", err)
	}

	return duration, nil
}

func SetCrossfadeDuration(guildID string, seconds float64) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var fadeOnStop int
	err := database.DB.QueryRow(
		`SELECT COALESCE(fade_on_stop, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&fadeOnStop)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get fade_on_stop: %w", err)
	}

	return fadeOnStop == 1, nil
}

func SetFadeOnStop(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
	var trimSilence int
	err := database.DB.QueryRow(
		`SELECT COALESCE(trim_silence, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&trimSilence)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get trim_silence: %w", err)
	}

	return trimSilence == 1, nil
}

func SetTrimSilence(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

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
