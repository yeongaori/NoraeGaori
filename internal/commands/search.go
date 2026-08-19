package commands

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

var (
	searchSelections   = make(map[string]bool) 
	searchSelectionsMu sync.Mutex
)

func HandleSearch(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	cmdOptions := i.ApplicationCommandData().Options
	if len(cmdOptions) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.EnterSearchQuery))
		return nil
	}
	query := cmdOptions[0].StringValue()

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel))
		return nil
	}

	loadingEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Queue.SearchingTitle, fmt.Sprintf(messages.T(i.GuildID).Queue.SearchingDesc, query))
	RespondEmbed(s, i, loadingEmbed)

	logger.Debugf("Starting search for query: \"%s\"", query)
	searchStartTime := time.Now()
	results, err := youtube.SearchMultiple(query, 10)
	searchEndTime := time.Now()
	logger.Debugf("Search completed in %dms for query: \"%s\", found %d results",
		searchEndTime.Sub(searchStartTime).Milliseconds(), query, len(results))

	if err != nil || len(results) == 0 {
		embed := messages.CreateErrorEmbed(messages.T(i.GuildID).Queue.NoResultsTitle, messages.T(i.GuildID).Queue.NoResultsDesc)
		UpdateResponseEmbed(s, i, embed)
		return nil
	}

	
	searchMessageID := fmt.Sprintf("%s_%s_%d", i.GuildID, i.Member.User.ID, time.Now().UnixNano())
	logger.Debugf("HandleSearch called, generated searchMessageID='%s'", searchMessageID)

	
	selectOptions := make([]discordgo.SelectMenuOption, 0, len(results))
	for idx, result := range results {
		titleWithNumber := fmt.Sprintf("%d. %s", idx+1, result.Title)
		label := truncateRunes(titleWithNumber, 100)

		selectOptions = append(selectOptions, discordgo.SelectMenuOption{
			Label:       label,
			Description: result.Duration,
			Value:       fmt.Sprintf("%s:%d", searchMessageID, idx),
		})
	}

	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       messages.T(i.GuildID).Queue.SearchResultsTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Queue.SearchResultsDesc, query),
		Fields:      make([]*discordgo.MessageEmbedField, 0, len(results)),
	}

	for idx, result := range results {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%d. %s", idx+1, result.Title),
			Value:  result.Duration,
			Inline: false,
		})
	}

	customID := fmt.Sprintf("search_select_%s", searchMessageID)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    customID,
					Placeholder: messages.T(i.GuildID).Queue.SelectPlaceholder,
					Options:     selectOptions,
				},
			},
		},
	}

	if err := UpdateResponseEmbedWithComponents(s, i, embed, components); err != nil {
		logger.Errorf("Failed to update message with results: %v", err)
		return err
	}

	go handleSearchSelection(s, i, results, customID, voiceState.ChannelID, searchMessageID)

	return nil
}

func parseSearchSelection(value string) (string, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("expected 2 parts, got %d", len(parts))
	}

	index, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid selection index %q: %w", parts[1], err)
	}

	return parts[0], index, nil
}

func handleSearchSelection(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, results []youtube.SearchResult, customID, voiceChannelID, searchMessageID string) {
	logger.Debugf("handleSearchSelection started, customID='%s', searchMessageID='%s'", customID, searchMessageID)
	timeout := time.After(30 * time.Second)
	done := make(chan struct{})

	defer func() {
		searchSelectionsMu.Lock()
		delete(searchSelections, searchMessageID)
		searchSelectionsMu.Unlock()
	}()

	selectionHandler := func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		logger.Debugf("selectionHandler triggered, captured searchMessageID='%s', captured customID='%s'", searchMessageID, customID)

		if i.Type != discordgo.InteractionMessageComponent {
			logger.Debugf("Not a message component, ignoring (type=%d)", i.Type)
			return
		}

		data := i.MessageComponentData()
		logger.Debugf("Message component received, data.CustomID='%s', checking against customID='%s'", data.CustomID, customID)

		if data.CustomID != customID {
			logger.Debugf("CustomID mismatch, ignoring (expected='%s', got='%s')", customID, data.CustomID)
			return
		}

		if i.Member.User.ID != originalInteraction.Member.User.ID {
			embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.NoPermission, messages.T(i.GuildID).Queue.OnlyRequester)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{embed},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		if len(data.Values) == 0 {
			logger.Warnf("No value in selection data")
			return
		}

		logger.Debugf("Parsing value: '%s'", data.Values[0])

		valueSearchID, selectedIndex, parseErr := parseSearchSelection(data.Values[0])
		if parseErr != nil {
			logger.Warnf("Invalid selection value %q: %v", data.Values[0], parseErr)
			return
		}

		logger.Debugf("Parsed value: valueSearchID='%s', selectedIndex=%d", valueSearchID, selectedIndex)

		if valueSearchID != searchMessageID {
			logger.Debugf("Value searchID mismatch: expected %s, got %s - ignoring", searchMessageID, valueSearchID)
			return
		}

		searchSelectionsMu.Lock()
		logger.Debugf("Checking duplicate map with searchMessageID='%s', already_selected=%v", searchMessageID, searchSelections[searchMessageID])
		if searchSelections[searchMessageID] {
			searchSelectionsMu.Unlock()
			logger.Debugf("Duplicate selection attempt ignored for search message: '%s'", searchMessageID)
			embed := messages.CreateWarningEmbed(messages.T(i.GuildID).Queue.AlreadySelectedTitle, messages.T(i.GuildID).Queue.AlreadySelectedDesc)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{embed},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
		logger.Debugf("Marking search as selected: searchMessageID='%s'", searchMessageID)
		searchSelections[searchMessageID] = true
		searchSelectionsMu.Unlock()

		if selectedIndex < 0 || selectedIndex >= len(results) {
			logger.Warnf("Invalid index %d for search with %d results", selectedIndex, len(results))
			return
		}

		selectedResult := results[selectedIndex]

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})

		processingEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Queue.ProcessingTitle,
			fmt.Sprintf(messages.T(i.GuildID).Queue.ProcessingDesc, selectedResult.Title))

		if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds:     &[]*discordgo.MessageEmbed{processingEmbed},
			Components: &[]discordgo.MessageComponent{},
		}); err != nil {
			logger.Errorf("Failed to edit message to processing state: %v", err)
		}

		videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", selectedResult.VideoID)
		logger.Debugf("Fetching detailed info for selected song: %s", selectedResult.Title)
		fetchStartTime := time.Now()

		song, err := youtube.GetVideoInfo(originalInteraction.GuildID, videoURL, originalInteraction.Member.User.Username, originalInteraction.Member.User.ID)
		if err != nil {
			logger.Errorf("Error fetching detailed info: %v", err)
			errorEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.SearchAddError)
			if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
				Components: &[]discordgo.MessageComponent{},
			}); err != nil {
				logger.Errorf("Failed to edit message to fetch-error state: %v", err)
			}
			return
		}

		fetchEndTime := time.Now()
		logger.Debugf("Detailed info fetched in %dms for: %s, uploader: %s",
			fetchEndTime.Sub(fetchStartTime).Milliseconds(), song.Title, song.Uploader)

		q, err := queue.GetQueue(originalInteraction.GuildID, false)
		if err != nil {
			logger.Errorf("Error getting queue: %v", err)
			return
		}

		if q == nil {
			if err := queue.CreateQueue(originalInteraction.GuildID, originalInteraction.ChannelID, voiceChannelID); err != nil {
				logger.Errorf("Error creating queue: %v", err)
				errorEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed)
				if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
					Components: &[]discordgo.MessageComponent{},
				}); err != nil {
					logger.Errorf("Failed to edit message to queue-create-error state: %v", err)
				}
				return
			}
			_, _ = queue.GetQueue(originalInteraction.GuildID, false)
		}

		queueSong := &queue.Song{
			URL:            song.URL,
			Title:          song.Title,
			Duration:       song.Duration,
			Thumbnail:      song.Thumbnail,
			Uploader:       song.Uploader,
			RequestedByID:  song.RequestedByID,
			RequestedByTag: song.RequestedBy,
			IsLive:         song.IsLive,
		}

		if err := queue.AddSong(originalInteraction.GuildID, queueSong, -1); err != nil {
			logger.Errorf("Error adding song to queue: %v", err)
			errorEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
				fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err))
			if _, editErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
				Components: &[]discordgo.MessageComponent{},
			}); editErr != nil {
				logger.Errorf("Failed to edit message to add-error state: %v", editErr)
			}
			return
		}

		addedEmbed := messages.CreateSongEmbed(
			i.GuildID,
			messages.ColorSuccess,
			messages.T(i.GuildID).Titles.Added,
			"",
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedBy,
			song.Thumbnail,
		)

		if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds:     &[]*discordgo.MessageEmbed{addedEmbed},
			Components: &[]discordgo.MessageComponent{},
		}); err != nil {
			logger.Errorf("Failed to edit message to added state: %v", err)
		}

		close(done)

		q, _ = queue.GetQueue(originalInteraction.GuildID, false)
		p := player.GetPlayer(originalInteraction.GuildID)
		if q != nil && len(q.Songs) == 1 && !p.Playing && !p.Loading {
			go player.Play(s, originalInteraction.GuildID)
		}
	}

	removeHandler := s.AddHandler(selectionHandler)
	defer removeHandler()

	select {
	case <-done:
		return
	case <-timeout:
	}

	timeoutEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Queue.SearchTimeoutTitle, messages.T(originalInteraction.GuildID).Queue.SearchTimeoutDesc)

	if _, err := s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{timeoutEmbed},
		Components: &[]discordgo.MessageComponent{},
	}); err != nil {
		logger.Errorf("Failed to edit message to timeout state: %v", err)
	}
}
