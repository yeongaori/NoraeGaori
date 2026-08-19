package queue

import (
	"database/sql"
	"fmt"

	"noraegaori/internal/database"
	"noraegaori/pkg/logger"
)

func invalidateLanguageCache(guildID string) {
	languageCacheMux.Lock()
	defer languageCacheMux.Unlock()
	delete(languageCache, guildID)
	delete(languageCacheLoaded, guildID)
}

func GetGuildLanguage(guildID string) (string, error) {
	if guildID == "" {
		return "", nil
	}

	languageCacheMux.RLock()
	if languageCacheLoaded[guildID] {
		val := languageCache[guildID]
		languageCacheMux.RUnlock()
		return val, nil
	}
	languageCacheMux.RUnlock()

	var lang sql.NullString
	err := database.DB.QueryRow(
		`SELECT language FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&lang)

	value := ""
	if err == nil && lang.Valid {
		value = lang.String
	} else if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("failed to get guild language: %w", err)
	}

	languageCacheMux.Lock()
	languageCache[guildID] = value
	languageCacheLoaded[guildID] = true
	languageCacheMux.Unlock()

	return value, nil
}

func SetGuildLanguage(guildID, lang string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	var langValue interface{}
	if lang == "" {
		langValue = nil
	} else {
		langValue = lang
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, language) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET language = ?`,
		guildID, langValue, langValue,
	)
	if err != nil {
		return fmt.Errorf("failed to set guild language: %w", err)
	}

	languageCacheMux.Lock()
	languageCache[guildID] = lang
	languageCacheLoaded[guildID] = true
	languageCacheMux.Unlock()

	InvalidateCache(guildID)
	logger.Debugf("Set language=%q for guild: %s", lang, guildID)
	return nil
}

func invalidatePrefixCache(guildID string) {
	prefixCacheMux.Lock()
	defer prefixCacheMux.Unlock()
	delete(prefixCache, guildID)
	delete(prefixCacheLoaded, guildID)
}

func GetGuildPrefix(guildID string) (string, error) {
	if guildID == "" {
		return "", nil
	}

	prefixCacheMux.RLock()
	if prefixCacheLoaded[guildID] {
		val := prefixCache[guildID]
		prefixCacheMux.RUnlock()
		return val, nil
	}
	prefixCacheMux.RUnlock()

	var prefix sql.NullString
	err := database.DB.QueryRow(
		`SELECT prefix FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&prefix)

	value := ""
	if err == nil && prefix.Valid {
		value = prefix.String
	} else if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("failed to get guild prefix: %w", err)
	}

	prefixCacheMux.Lock()
	prefixCache[guildID] = value
	prefixCacheLoaded[guildID] = true
	prefixCacheMux.Unlock()

	return value, nil
}

func SetGuildPrefix(guildID, prefix string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	var prefixValue interface{}
	if prefix == "" {
		prefixValue = nil
	} else {
		prefixValue = prefix
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, prefix) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET prefix = ?`,
		guildID, prefixValue, prefixValue,
	)
	if err != nil {
		return fmt.Errorf("failed to set guild prefix: %w", err)
	}

	prefixCacheMux.Lock()
	prefixCache[guildID] = prefix
	prefixCacheLoaded[guildID] = true
	prefixCacheMux.Unlock()

	InvalidateCache(guildID)
	logger.Debugf("Set prefix=%q for guild: %s", prefix, guildID)
	return nil
}
