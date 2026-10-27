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

func suggestTerms(request command.AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
	query := strings.TrimSpace(request.Query)
	if utf8.RuneCountInString(query) < minAutocompleteQueryRunes {
		return nil
	}

	language := messages.Lang(request.GuildID)
	normalized := normalizeQuery(query)
	key := autocompleteCacheKey("suggest", language, normalized)

	if cached, found := loadChoices(key); found {
		return cached
	}

	generation := beginAutocomplete(request.UserID, request.GuildID, request.CommandName)
	time.Sleep(autocompleteDebounceDelay)

	if !isLatestAutocomplete(request.UserID, request.GuildID, request.CommandName, generation) {
		return loadNearestChoices("suggest", language, normalized)
	}

	if cached, found := loadChoices(key); found {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteFetchTimeout)
	defer cancel()

	terms, err := youtube.SuggestTerms(ctx, query, language)
	if err != nil {
		logger.Debugf("Autocomplete suggest failed for %q: %v", query, err)
		return loadNearestChoices("suggest", language, normalized)
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

	saveChoices(key, choices)
	return choices
}

func suggestVideos(request command.AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
	query := strings.TrimSpace(request.Query)
	if youtube.IsYouTubeURL(query) {
		return nil
	}

	if utf8.RuneCountInString(query) < minAutocompleteQueryRunes {
		return nil
	}

	language := messages.Lang(request.GuildID)
	normalized := normalizeQuery(query)
	key := autocompleteCacheKey("video", language, normalized)

	if cached, found := loadChoices(key); found {
		return cached
	}

	generation := beginAutocomplete(request.UserID, request.GuildID, request.CommandName)
	time.Sleep(autocompleteDebounceDelay)

	if !isLatestAutocomplete(request.UserID, request.GuildID, request.CommandName, generation) {
		return loadNearestChoices("video", language, normalized)
	}

	if cached, found := loadChoices(key); found {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteFetchTimeout)
	defer cancel()

	results, err := youtube.SearchManyContext(ctx, query, command.MaxAutocompleteChoices)
	if err != nil {
		logger.Debugf("Autocomplete video search failed for %q: %v", query, err)
		return loadNearestChoices("video", language, normalized)
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, command.MaxAutocompleteChoices)
	for i := range results {
		result := &results[i]
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
			Name:  createVideoChoiceName(result.Title, result.Channel, result.Duration),
			Value: value,
		})
	}

	saveChoices(key, choices)
	return choices
}

func createVideoChoiceName(title, channel, duration string) string {
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
