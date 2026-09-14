package settings

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

type settingArguments struct {
	value       string
	hasValue    bool
	number      *settingSpec
	numberValue string
}

func HandleSetting(key string) func(*discordgo.Session, *discordgo.InteractionCreate) error {
	return settingCommandHandler(key, nil)
}

func HandleSettingWithEmbed(key string, buildEmbed func(guildID string) *discordgo.MessageEmbed) func(*discordgo.Session, *discordgo.InteractionCreate) error {
	return settingCommandHandler(key, buildEmbed)
}

func settingCommandHandler(key string, buildEmbed func(guildID string) *discordgo.MessageEmbed) func(*discordgo.Session, *discordgo.InteractionCreate) error {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) error {
		spec, found := findSetting(key)
		if !found {
			return fmt.Errorf("setting %q is not defined", key)
		}

		arguments := parseSettingArguments(spec, i.ApplicationCommandData().Options)
		if !arguments.hasValue && arguments.number == nil {
			return discord.RespondDropdownMenu(s, i, key)
		}

		if failed, err := applySettingArguments(i.GuildID, spec, &arguments); err != nil {
			if !isValidationError(err) {
				logger.Errorf("Failed to save setting %s for guild %s: %v", failed.key, i.GuildID, err)
			}
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, validationMessage(i.GuildID, failed, err)))
			return nil
		}

		if buildEmbed != nil {
			discord.RespondEmbed(s, i, buildEmbed(i.GuildID))
			return nil
		}
		return discord.RespondDropdownMenu(s, i, key)
	}
}

func parseSettingArguments(spec *settingSpec, options []*discordgo.ApplicationCommandInteractionDataOption) settingArguments {
	var arguments settingArguments

	for _, option := range options {
		text := optionText(option)

		if number := findNumberSetting(spec.key, option.Name); number != nil {
			arguments.number, arguments.numberValue = number, text
			continue
		}
		if value, err := normalizeValue(spec, text); err == nil {
			arguments.value, arguments.hasValue = value, true
			continue
		}
		if number := findNumberSetting(spec.key, ""); number != nil && isNumber(text) {
			arguments.number, arguments.numberValue = number, text
			arguments.value, arguments.hasValue = valueOn, true
		}
	}

	return arguments
}

func applySettingArguments(guildID string, spec *settingSpec, arguments *settingArguments) (*settingSpec, error) {
	if arguments.number != nil {
		normalized, err := normalizeValue(arguments.number, arguments.numberValue)
		if err != nil {
			return arguments.number, err
		}
		arguments.numberValue = normalized
	}

	if arguments.hasValue {
		if err := spec.write(guildID, arguments.value); err != nil {
			return spec, err
		}
	}
	if arguments.number != nil {
		if err := arguments.number.write(guildID, arguments.numberValue); err != nil {
			return arguments.number, err
		}
	}
	return nil, nil
}

func findNumberSetting(key, optionName string) *settingSpec {
	for index := range settingSpecs {
		spec := &settingSpecs[index]
		suffix, hasPrefix := strings.CutPrefix(spec.key, key)
		if spec.kind != settingNumber || !hasPrefix || !strings.HasPrefix(suffix, "_") {
			continue
		}
		if optionName == "" || suffix[1:] == optionName {
			return spec
		}
	}
	return nil
}

func optionText(option *discordgo.ApplicationCommandInteractionDataOption) string {
	if number, ok := option.Value.(float64); ok {
		return formatFloat(number)
	}
	text, _ := option.Value.(string)
	return text
}

func isNumber(text string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return err == nil
}

func isValidationError(err error) bool {
	return errors.Is(err, errNotNumber) || errors.Is(err, errNotInteger) || errors.Is(err, errOutOfRange) || errors.Is(err, errUnknownValue)
}
