package settings

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"noraegaori/internal/logger"
)

var (
	errNotNumber    = errors.New("value is not a number")
	errNotInteger   = errors.New("value is not a whole number")
	errOutOfRange   = errors.New("value is out of range")
	errTooLong      = errors.New("value is too long")
	errUnknownValue = errors.New("value is not an accepted choice")
)

func nextValue(spec *settingSpec, current string) string {
	if spec.kind != settingToggle {
		return current
	}
	if current == valueOn {
		return valueOff
	}
	return valueOn
}

func normalizeValue(spec *settingSpec, value string) (string, error) {
	value = strings.TrimSpace(value)

	switch spec.kind {
	case settingToggle:
		value = strings.ToLower(value)
		if value != valueOn && value != valueOff {
			return "", errUnknownValue
		}
		return value, nil
	case settingChoice:
		value = strings.ToLower(value)
		if alias, isAlias := spec.aliases[value]; isAlias {
			value = alias
		}
		if spec.hasDefault && value == defaultChoiceValue {
			return value, nil
		}
		if !slices.Contains(spec.options(), value) {
			return "", errUnknownValue
		}
		return value, nil
	case settingText:
		if len([]rune(value)) > int(spec.max) {
			return "", errTooLong
		}
		return value, nil
	case settingNumber:
		number, isNumber := parseNumber(value)
		if !isNumber {
			return "", errNotNumber
		}
		if number < spec.min || number > spec.max {
			return "", errOutOfRange
		}
		if spec.isInteger && number != math.Trunc(number) {
			return "", errNotInteger
		}
		return formatFloat(number), nil
	default:
		return value, nil
	}
}

func parseNumber(text string) (float64, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func applySetting(guildID string, spec *settingSpec, value string) error {
	normalized, err := normalizeValue(spec, value)
	if err != nil {
		return err
	}
	return spec.write(guildID, normalized)
}

func currentValue(guildID string, spec *settingSpec) (string, bool) {
	value, err := spec.read(guildID)
	if err != nil {
		logger.Errorf("Failed to read setting %s for guild %s: %v", spec.key, guildID, err)
		return "", false
	}
	return value, true
}
