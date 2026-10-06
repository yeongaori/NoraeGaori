package queue

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"noraegaori/internal/database"
	"noraegaori/internal/logger"
)

const (
	AutoMixStyleAuto       = "auto"
	autoMixOverridesColumn = "automix_overrides"
	overridePairSeparator  = ","
	overrideValueSeparator = "="
)

var legacyStyleColumns = []struct {
	category string
	column   string
}{
	{"volume", "automix_style_volume"},
	{"eq", "automix_style_eq"},
	{"filter", "automix_style_filter"},
	{"effect", "automix_style_effect"},
	{"loop", "automix_style_loop"},
}

func EncodeOverrides(overrides map[string]string) string {
	pairs := make([]string, 0, len(overrides))
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		pairs = append(pairs, key+overrideValueSeparator+overrides[key])
	}
	return strings.Join(pairs, overridePairSeparator)
}

func DecodeOverrides(encoded string) map[string]string {
	var overrides map[string]string
	for _, pair := range strings.Split(encoded, overridePairSeparator) {
		key, value, found := strings.Cut(pair, overrideValueSeparator)
		if !found || key == "" || value == "" || value == AutoMixStyleAuto {
			continue
		}
		if overrides == nil {
			overrides = make(map[string]string)
		}
		overrides[key] = value
	}
	return overrides
}

func ApplyOverrideChanges(overrides, changes map[string]string) map[string]string {
	merged := maps.Clone(overrides)
	if merged == nil {
		merged = make(map[string]string, len(changes))
	}
	for key, value := range changes {
		if value == "" || value == AutoMixStyleAuto {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	return merged
}

func ConvertLegacyStyles(expand func(category, value string) map[string]string) error {
	for _, table := range []string{"guild_settings", "songs"} {
		if err := convertLegacyTable(table, expand); err != nil {
			return err
		}
	}
	return nil
}

func convertLegacyTable(table string, expand func(category, value string) map[string]string) error {
	var categories, columns []string
	for _, legacy := range legacyStyleColumns {
		exists, err := database.ColumnExists(table, legacy.column)
		if err != nil {
			return err
		}
		if exists {
			categories = append(categories, legacy.category)
			columns = append(columns, legacy.column)
		}
	}
	if len(columns) == 0 {
		return nil
	}

	updates, err := legacyOverrides(table, categories, columns, expand)
	if err != nil {
		return err
	}
	for rowID, encoded := range updates {
		if _, err := database.DB.Exec(fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, table, autoMixOverridesColumn), encoded, rowID); err != nil {
			return fmt.Errorf("failed to convert %s styles: %w", table, err)
		}
	}
	for _, column := range columns {
		if _, err := database.DB.Exec(fmt.Sprintf(`ALTER TABLE %s DROP COLUMN %s`, table, column)); err != nil {
			return fmt.Errorf("failed to drop %s.%s: %w", table, column, err)
		}
	}
	logger.Debugf("Converted %d %s rows to %s", len(updates), table, autoMixOverridesColumn)
	return nil
}

func legacyOverrides(table string, categories, columns []string, expand func(category, value string) map[string]string) (map[int64]string, error) {
	selected := make([]string, 0, len(columns))
	for _, column := range columns {
		selected = append(selected, fmt.Sprintf("COALESCE(%s, '')", column))
	}
	rows, err := database.DB.Query(fmt.Sprintf(`SELECT rowid, COALESCE(%s, ''), %s FROM %s`,
		autoMixOverridesColumn, strings.Join(selected, ", "), table))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s styles: %w", table, err)
	}
	defer rows.Close()

	updates := make(map[int64]string)
	for rows.Next() {
		var rowID int64
		var stored string
		values := make([]string, len(categories))
		targets := []any{&rowID, &stored}
		for index := range values {
			targets = append(targets, &values[index])
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("failed to scan %s styles: %w", table, err)
		}

		overrides := DecodeOverrides(stored)
		for index, category := range categories {
			if values[index] != "" && values[index] != AutoMixStyleAuto {
				overrides = ApplyOverrideChanges(overrides, expand(category, values[index]))
			}
		}
		if encoded := EncodeOverrides(overrides); encoded != stored {
			updates[rowID] = encoded
		}
	}
	return updates, rows.Err()
}
