package play

import (
	"context"
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/youtube"
)

const (
	maxChoiceNameRunes        = 100
	maxChoiceValueRunes       = 100
	minAutocompleteQueryRunes = 2
	autocompleteFetchTimeout  = 1200 * time.Millisecond
	maxChannelSegmentRunes    = 40
	videoWatchURLPrefix       = "https://www.youtube.com/watch?v="
)

func autocompleteSuggestTerms(request command.AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
	query := strings.TrimSpace(request.Query)
	if utf8.RuneCountInString(query) < minAutocompleteQueryRunes {
		return nil
	}

	language := messages.Lang(request.GuildID)
	normalized := normalizeAutocompleteQuery(query)
	key := autocompleteCacheKey("suggest", language, normalized)

	if cached, found := loadAutocompleteChoices(key); found {
		return cached
	}

	generation := beginAutocompleteFetch(request.UserID, request.GuildID, request.CommandName)
	time.Sleep(autocompleteDebounceDelay)

	if !isLatestAutocompleteFetch(request.UserID, request.GuildID, request.CommandName, generation) {
		return loadNearestAutocompleteChoices("suggest", language, normalized)
	}

	if cached, found := loadAutocompleteChoices(key); found {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteFetchTimeout)
	defer cancel()

	terms, err := youtube.SuggestTerms(ctx, query, language)
	if err != nil {
		logger.Debugf("Autocomplete suggest failed for %q: %v", query, err)
		return loadNearestAutocompleteChoices("suggest", language, normalized)
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, command.MaxAutocompleteChoices)
	for _, term := range terms {
		if len(choices) >= command.MaxAutocompleteChoices {
			break
		}
		text := discord.TruncateRunes(term, maxChoiceValueRunes)
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  text,
			Value: text,
		})
	}

	saveAutocompleteChoices(key, choices)
	return choices
}

func autocompleteVideoResults(request command.AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
	query := strings.TrimSpace(request.Query)
	if youtube.IsYouTubeURL(query) {
		return nil
	}

	if utf8.RuneCountInString(query) < minAutocompleteQueryRunes {
		return nil
	}

	language := messages.Lang(request.GuildID)
	normalized := normalizeAutocompleteQuery(query)
	key := autocompleteCacheKey("video", language, normalized)

	if cached, found := loadAutocompleteChoices(key); found {
		return cached
	}

	generation := beginAutocompleteFetch(request.UserID, request.GuildID, request.CommandName)
	time.Sleep(autocompleteDebounceDelay)

	if !isLatestAutocompleteFetch(request.UserID, request.GuildID, request.CommandName, generation) {
		return loadNearestAutocompleteChoices("video", language, normalized)
	}

	if cached, found := loadAutocompleteChoices(key); found {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteFetchTimeout)
	defer cancel()

	results, err := youtube.SearchMultipleContext(ctx, query, command.MaxAutocompleteChoices)
	if err != nil {
		logger.Debugf("Autocomplete video search failed for %q: %v", query, err)
		return loadNearestAutocompleteChoices("video", language, normalized)
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, command.MaxAutocompleteChoices)
	for _, result := range results {
		if len(choices) >= command.MaxAutocompleteChoices {
			break
		}
		if result.VideoID == "" {
			continue
		}
		value := videoWatchURLPrefix + result.VideoID
		if utf8.RuneCountInString(value) > maxChoiceValueRunes {
			continue
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  buildVideoChoiceName(result.Title, result.Channel, result.Duration),
			Value: value,
		})
	}

	saveAutocompleteChoices(key, choices)
	return choices
}

func buildVideoChoiceName(title, channel, duration string) string {
	suffix := ""
	if channel != "" {
		suffix = " - " + discord.TruncateRunes(channel, maxChannelSegmentRunes)
	}
	if duration != "" {
		suffix += " (" + duration + ")"
	}

	titleBudget := maxChoiceNameRunes - utf8.RuneCountInString(suffix)
	if titleBudget < 10 {
		titleBudget = 10
	}

	name := discord.TruncateRunes(title, titleBudget) + suffix
	return discord.TruncateRunes(name, maxChoiceNameRunes)
}
