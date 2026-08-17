package commands

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

const (
	maxAutocompleteChoices    = 25
	maxChoiceNameRunes        = 100
	maxChoiceValueRunes       = 100
	minAutocompleteQueryRunes = 2
	autocompleteFetchTimeout  = 1200 * time.Millisecond
	maxChannelSegmentRunes    = 40
	videoWatchURLPrefix       = "https://www.youtube.com/watch?v="
)

type AutocompleteRequest struct {
	GuildID     string
	UserID      string
	CommandName string
	Query       string
}

func HandleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	cmd, exists := commands[data.Name]
	if !exists || cmd.AutocompleteHandler == nil {
		respondAutocompleteChoices(s, i, nil)
		return
	}

	request := AutocompleteRequest{
		GuildID:     i.GuildID,
		UserID:      interactionUserID(i),
		CommandName: data.Name,
		Query:       focusedStringOption(data.Options),
	}

	choices := cmd.AutocompleteHandler(request)
	if len(choices) > maxAutocompleteChoices {
		choices = choices[:maxAutocompleteChoices]
	}

	respondAutocompleteChoices(s, i, choices)
}

func focusedStringOption(options []*discordgo.ApplicationCommandInteractionDataOption) string {
	for _, option := range options {
		if option == nil || !option.Focused {
			continue
		}
		if option.Type != discordgo.ApplicationCommandOptionString {
			return ""
		}
		value, ok := option.Value.(string)
		if !ok {
			return ""
		}
		return value
	}
	return ""
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func respondAutocompleteChoices(s *discordgo.Session, i *discordgo.InteractionCreate, choices []*discordgo.ApplicationCommandOptionChoice) {
	if choices == nil {
		choices = []*discordgo.ApplicationCommandOptionChoice{}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		logger.Debugf("Failed to respond to autocomplete for %s: %v", i.ApplicationCommandData().Name, err)
	}
}

func autocompleteSuggestTerms(request AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
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

	if !allowAutocompleteFetch(request.UserID, request.CommandName) {
		return loadNearestAutocompleteChoices("suggest", language, normalized)
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteFetchTimeout)
	defer cancel()

	terms, err := youtube.SuggestTerms(ctx, query, language)
	if err != nil {
		logger.Debugf("Autocomplete suggest failed for %q: %v", query, err)
		return loadNearestAutocompleteChoices("suggest", language, normalized)
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, maxAutocompleteChoices)
	for _, term := range terms {
		if len(choices) >= maxAutocompleteChoices {
			break
		}
		text := truncateRunes(term, maxChoiceValueRunes)
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  text,
			Value: text,
		})
	}

	saveAutocompleteChoices(key, choices)
	return choices
}

func autocompleteVideoResults(request AutocompleteRequest) []*discordgo.ApplicationCommandOptionChoice {
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

	if !allowAutocompleteFetch(request.UserID, request.CommandName) {
		return loadNearestAutocompleteChoices("video", language, normalized)
	}

	ctx, cancel := context.WithTimeout(context.Background(), autocompleteFetchTimeout)
	defer cancel()

	results, err := youtube.SearchMultipleContext(ctx, query, maxAutocompleteChoices)
	if err != nil {
		logger.Debugf("Autocomplete video search failed for %q: %v", query, err)
		return loadNearestAutocompleteChoices("video", language, normalized)
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, maxAutocompleteChoices)
	for _, result := range results {
		if len(choices) >= maxAutocompleteChoices {
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
		suffix = " - " + truncateRunes(channel, maxChannelSegmentRunes)
	}
	if duration != "" {
		suffix += " (" + duration + ")"
	}

	titleBudget := maxChoiceNameRunes - utf8.RuneCountInString(suffix)
	if titleBudget < 10 {
		titleBudget = 10
	}

	name := truncateRunes(title, titleBudget) + suffix
	return truncateRunes(name, maxChoiceNameRunes)
}
