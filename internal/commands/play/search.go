package play

import (
	"fmt"
	"noraegaori/internal/discord"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

var (
	searchSelections   = make(map[string]bool)
	searchSelectionsMu sync.Mutex
)

func HandleSearch(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	cmdOptions := i.ApplicationCommandData().Options
	if len(cmdOptions) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Queue.EnterSearchQuery))
		return nil
	}
	query := cmdOptions[0].StringValue()

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel))
		return nil
	}

	loadingEmbed := messages.CreateWarningEmbed(messages.T(i.GuildID).Queue.SearchingTitle, fmt.Sprintf(messages.T(i.GuildID).Queue.SearchingDesc, query))
	discord.RespondEmbed(s, i, loadingEmbed)

	logger.Debugf("Starting search for query: \"%s\"", query)
	searchStartTime := time.Now()
	results, err := youtube.SearchMultiple(query, 10)
	searchEndTime := time.Now()
	logger.Debugf("Search completed in %dms for query: \"%s\", found %d results",
		searchEndTime.Sub(searchStartTime).Milliseconds(), query, len(results))

	if err != nil || len(results) == 0 {
		embed := messages.CreateErrorEmbed(messages.T(i.GuildID).Queue.NoResultsTitle, messages.T(i.GuildID).Queue.NoResultsDesc)
		discord.UpdateResponseEmbed(s, i, embed)
		return nil
	}

	searchMessageID := fmt.Sprintf("%s_%s_%d", i.GuildID, i.Member.User.ID, time.Now().UnixNano())
	logger.Debugf("HandleSearch called, generated searchMessageID='%s'", searchMessageID)

	selectOptions := make([]discordgo.SelectMenuOption, 0, len(results))
	for idx, result := range results {
		titleWithNumber := fmt.Sprintf("%d. %s", idx+1, result.Title)
		label := discord.TruncateRunes(titleWithNumber, 100)

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

	awaitSelection, cancelSelection := startSearchSelection(s, i, results, customID, voiceState.ChannelID, searchMessageID)

	if err := discord.UpdateResponseEmbedWithComponents(s, i, embed, components); err != nil {
		cancelSelection()
		logger.Errorf("Failed to update message with results: %v", err)
		return err
	}

	go awaitSelection()

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

type searchSelection struct {
	results         []youtube.SearchResult
	customID        string
	voiceChannelID  string
	searchMessageID string
	original        *discordgo.InteractionCreate
	done            chan struct{}
}

func (c *searchSelection) handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	logger.Debugf("selectionHandler triggered, captured c.searchMessageID='%s', captured c.customID='%s'", c.searchMessageID, c.customID)

	if i.Type != discordgo.InteractionMessageComponent {
		logger.Debugf("Not a message component, ignoring (type=%d)", i.Type)
		return
	}

	data := i.MessageComponentData()
	logger.Debugf("Message component received, data.CustomID='%s', checking against c.customID='%s'", data.CustomID, c.customID)

	if data.CustomID != c.customID {
		logger.Debugf("CustomID mismatch, ignoring (expected='%s', got='%s')", c.customID, data.CustomID)
		return
	}

	if i.Member.User.ID != c.original.Member.User.ID {
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

	if valueSearchID != c.searchMessageID {
		logger.Debugf("Value searchID mismatch: expected %s, got %s - ignoring", c.searchMessageID, valueSearchID)
		return
	}

	searchSelectionsMu.Lock()
	logger.Debugf("Checking duplicate map with c.searchMessageID='%s', already_selected=%v", c.searchMessageID, searchSelections[c.searchMessageID])
	if searchSelections[c.searchMessageID] {
		searchSelectionsMu.Unlock()
		logger.Debugf("Duplicate selection attempt ignored for search message: '%s'", c.searchMessageID)
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
	logger.Debugf("Marking search as selected: c.searchMessageID='%s'", c.searchMessageID)
	searchSelections[c.searchMessageID] = true
	searchSelectionsMu.Unlock()

	if selectedIndex < 0 || selectedIndex >= len(c.results) {
		logger.Warnf("Invalid index %d for search with %d c.results", selectedIndex, len(c.results))
		return
	}

	selectedResult := c.results[selectedIndex]

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

	song, err := youtube.GetVideoInfo(c.original.GuildID, videoURL, c.original.Member.User.Username, c.original.Member.User.ID)
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

	q, err := queue.GetQueue(c.original.GuildID, false)
	if err != nil {
		logger.Errorf("Error getting queue: %v", err)
		return
	}

	if q == nil {
		if err := queue.CreateQueue(c.original.GuildID, c.original.ChannelID, c.voiceChannelID); err != nil {
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
		_, _ = queue.GetQueue(c.original.GuildID, false)
	}

	queueSong := queueSongFrom(song)

	if err := queue.AddSong(c.original.GuildID, queueSong, -1); err != nil {
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

	close(c.done)

	player.ResumeOrStart(s, c.original.GuildID)
}

func startSearchSelection(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, results []youtube.SearchResult, customID, voiceChannelID, searchMessageID string) (func(), func()) {
	logger.Debugf("startSearchSelection registered, customID='%s', searchMessageID='%s'", customID, searchMessageID)

	selection := &searchSelection{
		results:         results,
		customID:        customID,
		voiceChannelID:  voiceChannelID,
		searchMessageID: searchMessageID,
		original:        originalInteraction,
		done:            make(chan struct{}),
	}

	removeHandler := s.AddHandler(selection.handle)
	cleanup := sync.OnceFunc(func() {
		removeHandler()
		searchSelectionsMu.Lock()
		delete(searchSelections, selection.searchMessageID)
		searchSelectionsMu.Unlock()
	})

	return func() { expireSearchSelection(s, selection, cleanup) }, cleanup
}

func expireSearchSelection(s *discordgo.Session, selection *searchSelection, cleanup func()) {
	defer cleanup()

	originalInteraction := selection.original

	select {
	case <-selection.done:
		return
	case <-time.After(30 * time.Second):
	}

	timeoutEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Queue.SearchTimeoutTitle, messages.T(originalInteraction.GuildID).Queue.SearchTimeoutDesc)

	if _, err := s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{timeoutEmbed},
		Components: &[]discordgo.MessageComponent{},
	}); err != nil {
		logger.Errorf("Failed to edit message to timeout state: %v", err)
	}
}
