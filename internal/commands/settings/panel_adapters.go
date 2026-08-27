package settings

import (
	"strconv"
	"strings"

	"noraegaori/internal/guild"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

const (
	valueOn  = "on"
	valueOff = "off"

	valueRepeatOff    = "off"
	valueRepeatAll    = "all"
	valueRepeatSingle = "single"

	defaultChoiceValue = "__default__"
	backValue          = "__back__"
)

var repeatValues = []string{valueRepeatOff, valueRepeatAll, valueRepeatSingle}

func boolValue(enabled bool) string {
	if enabled {
		return valueOn
	}
	return valueOff
}

func boolReader(get func(string) (bool, error)) func(string) (string, error) {
	return func(guildID string) (string, error) {
		enabled, err := get(guildID)
		if err != nil {
			return valueOff, err
		}
		return boolValue(enabled), nil
	}
}

func boolWriter(set func(string, bool) error) func(string, string) error {
	return func(guildID, value string) error {
		return set(guildID, value == valueOn)
	}
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func floatReader(get func(string) (float64, error)) func(string) (string, error) {
	return func(guildID string) (string, error) {
		value, err := get(guildID)
		if err != nil {
			return "", err
		}
		return formatFloat(value), nil
	}
}

func floatWriter(set func(string, float64) error) func(string, string) error {
	return func(guildID, value string) error {
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return errNotNumber
		}
		return set(guildID, number)
	}
}

func readPrefix(guildID string) (string, error) {
	return guild.GetPrefix(guildID)
}

func writePrefix(guildID, value string) error {
	return guild.SetPrefix(guildID, value)
}

func readLanguage(guildID string) (string, error) {
	return guild.GetLanguage(guildID)
}

func writeLanguage(guildID, value string) error {
	if value == defaultChoiceValue {
		value = ""
	}
	return guild.SetLanguage(guildID, value)
}

func readVolume(guildID string) (string, error) {
	return floatReader(queue.GetVolume)(guildID)
}

func writeVolume(guildID, value string) error {
	return floatWriter(player.SetVolume)(guildID, value)
}

func readAutoMixBeats(guildID string) (string, error) {
	beats, err := queue.GetAutoMixBeats(guildID)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(beats), nil
}

func writeAutoMixBeats(guildID, value string) error {
	beats, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return errNotNumber
	}
	return queue.SetAutoMixBeats(guildID, int(beats))
}

func readRepeat(guildID string) (string, error) {
	mode, err := queue.GetRepeatMode(guildID)
	if err != nil {
		return valueRepeatOff, err
	}
	switch mode {
	case queue.RepeatAll:
		return valueRepeatAll, nil
	case queue.RepeatSingle:
		return valueRepeatSingle, nil
	default:
		return valueRepeatOff, nil
	}
}

func writeRepeat(guildID, value string) error {
	switch strings.TrimSpace(value) {
	case valueRepeatAll:
		return queue.SetRepeatMode(guildID, queue.RepeatAll)
	case valueRepeatSingle:
		return queue.SetRepeatMode(guildID, queue.RepeatSingle)
	default:
		return queue.SetRepeatMode(guildID, queue.RepeatOff)
	}
}
