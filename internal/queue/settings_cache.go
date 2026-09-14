package queue

import (
	"database/sql"
	"fmt"
	"time"

	"noraegaori/internal/database"
)

type settingsCacheEntry struct {
	settings  guildSettingsRow
	db        *sql.DB
	timestamp time.Time
}

var (
	settingsCache       = make(map[string]*settingsCacheEntry)
	settingsGenerations = make(map[string]uint64)
)

func cachedGuildSettings(guildID string) (*guildSettingsRow, error) {
	cacheMux.RLock()
	entry := settingsCache[guildID]
	generation := settingsGenerations[guildID]
	cacheMux.RUnlock()

	if entry != nil && entry.db == database.DB && time.Since(entry.timestamp) < cacheTTL {
		return &entry.settings, nil
	}

	settings, err := loadGuildSettingsRow(guildID)
	if err != nil {
		return nil, err
	}
	storeGuildSettings(guildID, generation, settings)
	return &settings, nil
}

func settingsGeneration(guildID string) uint64 {
	cacheMux.RLock()
	defer cacheMux.RUnlock()
	return settingsGenerations[guildID]
}

func storeGuildSettings(guildID string, generation uint64, settings guildSettingsRow) {
	cacheMux.Lock()
	defer cacheMux.Unlock()

	if settingsGenerations[guildID] != generation {
		return
	}
	settingsCache[guildID] = &settingsCacheEntry{settings: settings, db: database.DB, timestamp: time.Now()}
}

func readSetting[T any](guildID, name string, fallback T, pick func(*guildSettingsRow) T) (T, error) {
	settings, err := cachedGuildSettings(guildID)
	if err != nil {
		return fallback, fmt.Errorf("failed to get %s: %w", name, err)
	}
	return pick(settings), nil
}

func autoMixStyleOf(settings *guildSettingsRow, category string) string {
	switch category {
	case "volume":
		return settings.styleVolume
	case "eq":
		return settings.styleEQ
	case "filter":
		return settings.styleFilter
	case "effect":
		return settings.styleEffect
	default:
		return settings.styleLoop
	}
}
