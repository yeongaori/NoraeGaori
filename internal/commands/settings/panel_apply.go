package settings

import (
	"errors"
	"strconv"
	"strings"

	"noraegaori/internal/logger"
)

var (
	errNotNumber  = errors.New("value is not a number")
	errOutOfRange = errors.New("value is out of range")
	errTooLong    = errors.New("value is too long")
)

func nextValue(spec settingSpec, current string) string {
	switch spec.kind {
	case settingToggle:
		if current == valueOn {
			return valueOff
		}
		return valueOn
	case settingCycle:
		for index, value := range repeatValues {
			if value == current {
				return repeatValues[(index+1)%len(repeatValues)]
			}
		}
		return repeatValues[0]
	default:
		return current
	}
}

func normalizeValue(spec settingSpec, value string) (string, error) {
	value = strings.TrimSpace(value)

	switch spec.kind {
	case settingText:
		if len([]rune(value)) > int(spec.max) {
			return "", errTooLong
		}
		return value, nil
	case settingNumber:
		if value == "" {
			return "", errNotNumber
		}
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "", errNotNumber
		}
		if number < spec.min || number > spec.max {
			return "", errOutOfRange
		}
		return formatFloat(number), nil
	default:
		return value, nil
	}
}

func applySetting(guildID string, spec settingSpec, value string) error {
	normalized, err := normalizeValue(spec, value)
	if err != nil {
		return err
	}
	return spec.write(guildID, normalized)
}

func currentValue(guildID string, spec settingSpec) (string, bool) {
	value, err := spec.read(guildID)
	if err != nil {
		logger.Errorf("Failed to read setting %s for guild %s: %v", spec.key, guildID, err)
		return "", false
	}
	return value, true
}
