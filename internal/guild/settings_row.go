package guild

import (
	"fmt"
	"sync"

	"noraegaori/internal/config"
	"noraegaori/internal/database"
)

var (
	settingsChangeCallbacks []func(guildID string)
	settingsChangeMux       sync.Mutex
)

func OnSettingsChange(fn func(guildID string)) {
	settingsChangeMux.Lock()
	defer settingsChangeMux.Unlock()
	settingsChangeCallbacks = append(settingsChangeCallbacks, fn)
}

func SaveSetting(guildID, column string, value any) error {
	if _, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, volume) VALUES (?, ?) ON CONFLICT(guild_id) DO NOTHING`,
		guildID, config.DefaultVolume(),
	); err != nil {
		return fmt.Errorf("failed to create the settings row: %w", err)
	}

	if _, err := database.DB.Exec(fmt.Sprintf(`UPDATE guild_settings SET %s = ? WHERE guild_id = ?`, column), value, guildID); err != nil {
		return fmt.Errorf("failed to update %s: %w", column, err)
	}

	notifySettingsChange(guildID)
	return nil
}

func notifySettingsChange(guildID string) {
	settingsChangeMux.Lock()
	callbacks := make([]func(string), len(settingsChangeCallbacks))
	copy(callbacks, settingsChangeCallbacks)
	settingsChangeMux.Unlock()

	for _, callback := range callbacks {
		callback(guildID)
	}
}
