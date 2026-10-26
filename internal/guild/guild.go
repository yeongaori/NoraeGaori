package guild

import (
	"database/sql"
	"fmt"
	"sync"

	"noraegaori/internal/database"
	"noraegaori/internal/logger"
)

type settingCache struct {
	column   string
	mu       sync.RWMutex
	values   map[string]string
	isLoaded map[string]bool
}

var (
	prefixCache   = settingCache{column: "prefix", values: make(map[string]string), isLoaded: make(map[string]bool)}
	languageCache = settingCache{column: "language", values: make(map[string]string), isLoaded: make(map[string]bool)}
)

func (cache *settingCache) invalidate(guildID string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	delete(cache.values, guildID)
	delete(cache.isLoaded, guildID)
}

func (cache *settingCache) load(guildID string) (string, error) {
	if guildID == "" {
		return "", nil
	}

	cache.mu.RLock()
	if cache.isLoaded[guildID] {
		val := cache.values[guildID]
		cache.mu.RUnlock()
		return val, nil
	}
	cache.mu.RUnlock()

	var stored sql.NullString
	err := database.DB.QueryRow(
		fmt.Sprintf(`SELECT %s FROM guild_settings WHERE guild_id = ?`, cache.column),
		guildID,
	).Scan(&stored)

	value := ""
	if err == nil && stored.Valid {
		value = stored.String
	} else if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("failed to get guild %s: %w", cache.column, err)
	}

	cache.mu.Lock()
	cache.values[guildID] = value
	cache.isLoaded[guildID] = true
	cache.mu.Unlock()

	return value, nil
}

func (cache *settingCache) save(guildID, value string) error {
	release := AcquireLock(guildID)
	defer release()

	var storedValue interface{}
	if value != "" {
		storedValue = value
	}

	if err := SaveSetting(guildID, cache.column, storedValue); err != nil {
		return fmt.Errorf("failed to set guild %s: %w", cache.column, err)
	}

	cache.mu.Lock()
	cache.values[guildID] = value
	cache.isLoaded[guildID] = true
	cache.mu.Unlock()

	logger.Debugf("Set %s=%q for guild: %s", cache.column, value, guildID)
	return nil
}

func GetLanguage(guildID string) (string, error) {
	return languageCache.load(guildID)
}

func SetLanguage(guildID, lang string) error {
	return languageCache.save(guildID, lang)
}

func InvalidateCaches(guildID string) {
	prefixCache.invalidate(guildID)
	languageCache.invalidate(guildID)
}

func GetPrefix(guildID string) (string, error) {
	return prefixCache.load(guildID)
}

func SetPrefix(guildID, prefix string) error {
	return prefixCache.save(guildID, prefix)
}
