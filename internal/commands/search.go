package commands

import (
	"fmt"
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

// searchSelections prevents the same result from being queued twice on duplicate select-menu callbacks.
var (
	searchSelections   = make(map[string]bool) // searchMessageID → selected
	searchSelectionsMu sync.Mutex
)

// HandleSearch shows 10 YouTube results in a select menu and queues the chosen song.
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

	logger.Debugf("[Search] Starting search for query: \"%s\"", query)
	searchStartTime := time.Now()
	results, err := youtube.SearchMultiple(query, 10)
	searchEndTime := time.Now()
	logger.Debugf("[Search] Search completed in %dms for query: \"%s\", found %d results",
		searchEndTime.Sub(searchStartTime).Milliseconds(), query, len(results))

	if err != nil || len(results) == 0 {
		embed := messages.CreateErrorEmbed(messages.T(i.GuildID).Queue.NoResultsTitle, messages.T(i.GuildID).Queue.NoResultsDesc)
		UpdateResponseEmbed(s, i, embed)
		return nil
	}

	// Use guild+user+nano for uniqueness — i.ID is the interaction ID, not the message ID.
	searchMessageID := fmt.Sprintf("%s_%s_%d", i.GuildID, i.Member.User.ID, time.Now().UnixNano())
	logger.Debugf("[Search] HandleSearch called, generated searchMessageID='%s'", searchMessageID)

	// Each select-menu value encodes "{searchMessageID}:{index}" to validate the session on selection.
	selectOptions := make([]discordgo.SelectMenuOption, 0, len(results))
	for idx, result := range results {
		titleWithNumber := fmt.Sprintf("%d. %s", idx+1, result.Title)
		label := titleWithNumber
		if len(label) > 100 {
			label = titleWithNumber[:97] + "..."
		}

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
		logger.Errorf("[Search] Failed to update message with results: %v", err)
		return err
	}

	go handleSearchSelection(s, i, results, customID, voiceState.ChannelID, searchMessageID)

	return nil
}

func handleSearchSelection(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, results []youtube.SearchResult, customID, voiceChannelID, searchMessageID string) {
	logger.Debugf("[Search] handleSearchSelection started, customID='%s', searchMessageID='%s'", customID, searchMessageID)
	timeout := time.After(30 * time.Second)

	defer func() {
		searchSelectionsMu.Lock()
		delete(searchSelections, searchMessageID)
		searchSelectionsMu.Unlock()
	}()

	selectionHandler := func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		logger.Debugf("[Search] selectionHandler triggered, captured searchMessageID='%s', captured customID='%s'", searchMessageID, customID)

		if i.Type != discordgo.InteractionMessageComponent {
			logger.Debugf("[Search] Not a message component, ignoring (type=%d)", i.Type)
			return
		}

		data := i.MessageComponentData()
		logger.Debugf("[Search] Message component received, data.CustomID='%s', checking against customID='%s'", data.CustomID, customID)

		if data.CustomID != customID {
			logger.Debugf("[Search] CustomID mismatch, ignoring (expected='%s', got='%s')", customID, data.CustomID)
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
			logger.Warnf("[Search] No value in selection data")
			return
		}

		logger.Debugf("[Search] Parsing value: '%s'", data.Values[0])

		var valueSearchID string
		var selectedIndex int
		parts := strings.Split(data.Values[0], ":")
		if len(parts) != 2 {
			logger.Warnf("[Search] Invalid value format: %s (expected 2 parts, got %d)", data.Values[0], len(parts))
			return
		}

		valueSearchID = parts[0]
		fmt.Sscanf(parts[1], "%d", &selectedIndex)

		logger.Debugf("[Search] Parsed value: valueSearchID='%s', selectedIndex=%d", valueSearchID, selectedIndex)

		if valueSearchID != searchMessageID {
			logger.Debugf("[Search] Value searchID mismatch: expected %s, got %s - ignoring", searchMessageID, valueSearchID)
			return
		}

		searchSelectionsMu.Lock()
		logger.Debugf("[Search] Checking duplicate map with searchMessageID='%s', already_selected=%v", searchMessageID, searchSelections[searchMessageID])
		if searchSelections[searchMessageID] {
			searchSelectionsMu.Unlock()
			logger.Debugf("[Search] Duplicate selection attempt ignored for search message: '%s'", searchMessageID)
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
		logger.Debugf("[Search] Marking search as selected: searchMessageID='%s'", searchMessageID)
		searchSelections[searchMessageID] = true
		searchSelectionsMu.Unlock()

		if selectedIndex < 0 || selectedIndex >= len(results) {
			logger.Warnf("[Search] Invalid index %d for search with %d results", selectedIndex, len(results))
			return
		}

		selectedResult := results[selectedIndex]

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})

		processingEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Queue.ProcessingTitle,
			fmt.Sprintf(messages.T(i.GuildID).Queue.ProcessingDesc, selectedResult.Title))

		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds:     &[]*discordgo.MessageEmbed{processingEmbed},
			Components: &[]discordgo.MessageComponent{},
		})

		videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", selectedResult.VideoID)
		logger.Debugf("[Search] Fetching detailed info for selected song: %s", selectedResult.Title)
		fetchStartTime := time.Now()

		song, err := youtube.GetVideoInfo(originalInteraction.GuildID, videoURL, originalInteraction.Member.User.Username, originalInteraction.Member.User.ID)
		if err != nil {
			logger.Errorf("[Search] Error fetching detailed info: %v", err)
			errorEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.SearchAddError)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
				Components: &[]discordgo.MessageComponent{},
			})
			return
		}

		fetchEndTime := time.Now()
		logger.Debugf("[Search] Detailed info fetched in %dms for: %s, uploader: %s",
			fetchEndTime.Sub(fetchStartTime).Milliseconds(), song.Title, song.Uploader)

		q, err := queue.GetQueue(originalInteraction.GuildID, false)
		if err != nil {
			logger.Errorf("[Search] Error getting queue: %v", err)
			return
		}

		if q == nil {
			if err := queue.CreateQueue(originalInteraction.GuildID, originalInteraction.ChannelID, voiceChannelID); err != nil {
				logger.Errorf("[Search] Error creating queue: %v", err)
				errorEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed)
				s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Embeds: &[]*discordgo.MessageEmbed{errorEmbed},
					Components: &[]discordgo.MessageComponent{},
				})
				return
			}
			q, _ = queue.GetQueue(originalInteraction.GuildID, false)
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
			logger.Errorf("[Search] Error adding song to queue: %v", err)
			errorEmbed := messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
				fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err))
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
				Components: &[]discordgo.MessageComponent{},
			})
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

		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds:     &[]*discordgo.MessageEmbed{addedEmbed},
			Components: &[]discordgo.MessageComponent{},
		})

		q, _ = queue.GetQueue(originalInteraction.GuildID, false)
		p := player.GetPlayer(originalInteraction.GuildID)
		if q != nil && len(q.Songs) == 1 && !p.Playing && !p.Loading {
			go player.Play(s, originalInteraction.GuildID)
		}
	}

	removeHandler := s.AddHandler(selectionHandler)
	defer removeHandler()

	<-timeout

	timeoutEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Queue.SearchTimeoutTitle, messages.T(originalInteraction.GuildID).Queue.SearchTimeoutDesc)

	s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{timeoutEmbed},
		Components: &[]discordgo.MessageComponent{},
	})
}
