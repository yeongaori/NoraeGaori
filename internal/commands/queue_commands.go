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

// Track search selections per message ID to prevent duplicates
var (
	searchSelections   = make(map[string]bool) // messageID -> selected
	searchSelectionsMu sync.Mutex
)

// HandleQueue handles the queue command with pagination
func HandleQueue(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleEmptyQueue, messages.DescEmptyQueue))
		return nil
	}

	// Pagination settings
	const songsPerPage = 10
	totalSongs := len(q.Songs)
	totalPages := (totalSongs + songsPerPage - 1) / songsPerPage
	currentPage := 1

	// Get page from options if provided
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		currentPage = int(options[0].IntValue())
		if currentPage < 1 {
			currentPage = 1
		}
		if currentPage > totalPages {
			currentPage = totalPages
		}
	}

	// Create embed for current page
	embed := createQueueEmbed(q.Songs, currentPage, totalPages, songsPerPage)

	// If only one page, no need for buttons
	if totalPages == 1 {
		RespondEmbed(s, i, embed)
		return nil
	}

	// Create navigation buttons
	components := createQueueButtons(currentPage, totalPages)

	// Send response with buttons using the new helper
	msg, err := RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		logger.Errorf("[Queue] Failed to send response: %v", err)
		return err
	}

	// Start button collector (5 minute timeout)
	go handleQueueButtons(s, i, msg, i.GuildID, totalPages, songsPerPage)

	return nil
}

// createQueueEmbed creates the queue embed for a specific page
func createQueueEmbed(songs []*queue.Song, page, totalPages, perPage int) *discordgo.MessageEmbed {
	start := (page - 1) * perPage
	end := start + perPage
	if end > len(songs) {
		end = len(songs)
	}

	var description strings.Builder

	for idx := start; idx < end; idx++ {
		song := songs[idx]

		duration := song.Duration
		if song.IsLive {
			duration = messages.T().Queue.LiveBadge
		}

		if idx == 0 {
			// Current playing song with ▶️
			description.WriteString(fmt.Sprintf("▶️ **[%s](%s)**\n   %s: %s | %s: %s | %s: %s\n\n",
				messages.EscapeMarkdown(song.Title),
				song.URL,
				messages.FieldUploader, messages.EscapeMarkdown(song.Uploader),
				messages.FieldDuration, duration,
				messages.FieldRequester, messages.EscapeMarkdown(song.RequestedByTag),
			))
		} else {
			// Queue songs with number
			description.WriteString(fmt.Sprintf("%d. **[%s](%s)**\n   %s: %s | %s: %s | %s: %s\n\n",
				idx+1,
				messages.EscapeMarkdown(song.Title),
				song.URL,
				messages.FieldUploader, messages.EscapeMarkdown(song.Uploader),
				messages.FieldDuration, duration,
				messages.FieldRequester, messages.EscapeMarkdown(song.RequestedByTag),
			))
		}
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       messages.TitleQueue,
		Description: description.String(),
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(messages.FooterPagination, page, totalPages, len(songs)),
		},
	}
}

// createQueueButtons creates Previous/Next buttons for queue pagination
func createQueueButtons(page, totalPages int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    messages.ButtonPrevious,
					Style:    discordgo.PrimaryButton,
					CustomID: "queue_prev",
					Disabled: page == 1,
				},
				discordgo.Button{
					Label:    messages.T().Queue.QueueNextButton,
					Style:    discordgo.PrimaryButton,
					CustomID: "queue_next",
					Disabled: page == totalPages,
				},
			},
		},
	}
}

// handleQueueButtons handles button interactions for queue pagination
func handleQueueButtons(s *discordgo.Session, i *discordgo.InteractionCreate, originalMsg *discordgo.Message, guildID string, totalPages, perPage int) {
	timeout := time.After(5 * time.Minute)
	currentPage := 1
	var pageMu sync.Mutex // Protect currentPage from concurrent access

	// Get initial page from options
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		currentPage = int(options[0].IntValue())
	}

	// Get message ID for comparison
	originalMsgID := ""
	if originalMsg != nil {
		originalMsgID = originalMsg.ID
	}

	// Create a channel for button events
	buttonHandler := func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic.Type != discordgo.InteractionMessageComponent {
			return
		}

		// Check if this is our message (if we have a message ID to check against)
		if originalMsgID != "" && (ic.Message == nil || ic.Message.ID != originalMsgID) {
			return
		}

		data := ic.MessageComponentData()

		// Check if it's a queue button by CustomID
		if data.CustomID != "queue_prev" && data.CustomID != "queue_next" {
			return
		}

		// Protect currentPage with mutex
		pageMu.Lock()
		switch data.CustomID {
		case "queue_prev":
			if currentPage > 1 {
				currentPage--
			}
		case "queue_next":
			if currentPage < totalPages {
				currentPage++
			}
		default:
			pageMu.Unlock()
			return
		}
		page := currentPage
		pageMu.Unlock()

		// Get updated queue
		q, err := queue.GetQueue(guildID, false)
		if err != nil || q == nil || len(q.Songs) == 0 {
			return
		}

		// Update embed
		embed := createQueueEmbed(q.Songs, page, totalPages, perPage)
		components := createQueueButtons(page, totalPages)

		// Respond to button interaction
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	}

	// Register handler
	removeHandler := s.AddHandler(buttonHandler)
	defer removeHandler()

	// Wait for timeout
	<-timeout

	// Remove buttons after timeout
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || originalMsg == nil {
		return
	}

	// Get final page value safely
	pageMu.Lock()
	finalPage := currentPage
	pageMu.Unlock()

	embed := createQueueEmbed(q.Songs, finalPage, totalPages, perPage)

	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         originalMsg.ID,
		Channel:    originalMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{}, // Remove buttons
	})
}

// HandleRemove handles the remove command
func HandleRemove(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.EnterPosition))
		return nil
	}
	positionStr := options[0].StringValue()

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleEmptyQueue, messages.DescEmptyQueue))
		return nil
	}

	// Check for "ALL" keyword
	if strings.ToUpper(positionStr) == "ALL" {
		return handleRemoveAll(s, i, q)
	}

	// Check for range (e.g., "1-5")
	if strings.Contains(positionStr, "-") {
		return handleRemoveRange(s, i, q, positionStr)
	}

	// Single position removal
	return handleRemoveSingle(s, i, q, positionStr)
}

// handleRemoveAll removes all songs from the requesting user (except currently playing)
func handleRemoveAll(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue) error {
	userID := i.Member.User.ID

	// Find all user's songs (excluding currently playing if it's playing/loading)
	var userSongs []*queue.Song
	startIdx := 0
	if q.Playing || q.Loading {
		startIdx = 1 // Skip currently playing song
	}

	for idx := startIdx; idx < len(q.Songs); idx++ {
		if q.Songs[idx].RequestedByID == userID {
			userSongs = append(userSongs, q.Songs[idx])
		}
	}

	if len(userSongs) == 0 {
		description := messages.T().Queue.NoUserSongs

		// Check if user only has the currently playing song
		if (q.Playing || q.Loading) && len(q.Songs) > 0 && q.Songs[0].RequestedByID == userID {
			description = messages.T().Queue.OnlyCurrentSong
		}

		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Queue.NoSongsToRemoveTitle, description))
		return nil
	}

	// Check if next song (position 1) is being removed
	isNextSongRemoved := false
	if len(q.Songs) > 1 {
		nextSong := q.Songs[1]
		for _, song := range userSongs {
			if song.ID == nextSong.ID {
				isNextSongRemoved = true
				break
			}
		}
	}

	// Get song IDs to remove
	songIDs := make([]int, len(userSongs))
	for i, song := range userSongs {
		songIDs[i] = song.ID
	}

	// Remove from queue
	if err := queue.RemoveSongsByIDs(i.GuildID, songIDs); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Queue.RemoveFailed, err)))
		return err
	}

	// Cleanup pre-cache worker if next song was removed
	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T().Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T().Queue.SongsRemovedAll, i.Member.User.Username, len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}

// handleRemoveRange removes songs in a range (e.g., "1-5")
func handleRemoveRange(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, rangeStr string) error {
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.InvalidRange))
		return nil
	}

	start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))

	if err1 != nil || err2 != nil || start < 1 || end < start || start > len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.InvalidRange))
		return nil
	}

	// Prevent removing currently playing song (position 1) in range
	if start == 1 && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Queue.RangeIncludesCurrent))
		return nil
	}

	// Adjust end to actual queue length
	if end > len(q.Songs) {
		end = len(q.Songs)
	}

	// Get songs in range (convert to 0-based index)
	songsToRemove := q.Songs[start-1 : end]

	// Filter by ownership - only remove user's songs
	userID := i.Member.User.ID
	var userSongs []*queue.Song
	for _, song := range songsToRemove {
		if song.RequestedByID == userID {
			userSongs = append(userSongs, song)
		}
	}

	if len(userSongs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoPermission, messages.T().Queue.NoUserSongsInRange))
		return nil
	}

	// Check if next song (position 1) is being removed
	isNextSongRemoved := false
	if len(q.Songs) > 1 {
		nextSong := q.Songs[1]
		for _, song := range userSongs {
			if song.ID == nextSong.ID {
				isNextSongRemoved = true
				break
			}
		}
	}

	// Get song IDs to remove
	songIDs := make([]int, len(userSongs))
	for i, song := range userSongs {
		songIDs[i] = song.ID
	}

	// Remove from queue
	if err := queue.RemoveSongsByIDs(i.GuildID, songIDs); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Queue.RemoveFailed, err)))
		return err
	}

	// Cleanup pre-cache worker if next song was removed
	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T().Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T().Queue.RangeRemoved, start, end, len(userSongs)))
	RespondEmbed(s, i, embed)
	return nil
}

// handleRemoveSingle removes a single song at specified position
func handleRemoveSingle(s *discordgo.Session, i *discordgo.InteractionCreate, q *queue.Queue, positionStr string) error {
	position, err := strconv.Atoi(strings.TrimSpace(positionStr))
	if err != nil || position < 1 || position > len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	// Prevent removing currently playing song (position 1)
	if position == 1 && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Queue.CannotRemoveCurrent))
		return nil
	}

	songToRemove := q.Songs[position-1]

	// Check ownership
	if songToRemove.RequestedByID != i.Member.User.ID {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoPermission, messages.T().Queue.OnlyOwnSongs))
		return nil
	}

	// Check if next song (position 2, index 1) is being removed
	isNextSongRemoved := len(q.Songs) > 1 && q.Songs[1].ID == songToRemove.ID

	// Remove song (convert to 0-based index)
	if err := queue.RemoveSong(i.GuildID, position-1); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Queue.RemoveFailed, err)))
		return err
	}

	// Cleanup pre-cache worker if next song was removed
	if isNextSongRemoved {
		player.CleanupPreCacheWorker(i.GuildID)
	}

	embed := messages.CreateSuccessEmbed(messages.T().Queue.SongsRemovedTitle,
		fmt.Sprintf(messages.T().Queue.SongRemoved, messages.EscapeMarkdown(songToRemove.Title)))
	RespondEmbed(s, i, embed)
	return nil
}

// HandleClear handles the clear command
func HandleClear(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// This is essentially the same as stop
	return HandleStop(s, i)
}

// HandleSearch handles the search command
func HandleSearch(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	cmdOptions := i.ApplicationCommandData().Options
	if len(cmdOptions) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.EnterSearchQuery))
		return nil
	}
	query := cmdOptions[0].StringValue()

	// Check if user is in voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorNotInVoiceChannel))
		return nil
	}

	// Show loading message first
	loadingEmbed := messages.CreateWarningEmbed(messages.T().Queue.SearchingTitle, fmt.Sprintf(messages.T().Queue.SearchingDesc, query))
	RespondEmbed(s, i, loadingEmbed)

	// Search YouTube for 10 results
	logger.Debugf("[Search] Starting search for query: \"%s\"", query)
	searchStartTime := time.Now()
	results, err := youtube.SearchMultiple(query, 10)
	searchEndTime := time.Now()
	logger.Debugf("[Search] Search completed in %dms for query: \"%s\", found %d results",
		searchEndTime.Sub(searchStartTime).Milliseconds(), query, len(results))

	if err != nil || len(results) == 0 {
		embed := messages.CreateErrorEmbed(messages.T().Queue.NoResultsTitle, messages.T().Queue.NoResultsDesc)
		UpdateResponseEmbed(s, i, embed)
		return nil
	}

	// Generate unique search ID (using guild, user, and timestamp for guaranteed uniqueness)
	// We can't rely on i.ID because that's the interaction ID, not the message ID
	searchMessageID := fmt.Sprintf("%s_%s_%d", i.GuildID, i.Member.User.ID, time.Now().UnixNano())
	logger.Debugf("[Search] HandleSearch called, generated searchMessageID='%s'", searchMessageID)

	// Create select menu options with globally unique values
	// Each value is "{searchMessageID}:{index}" to ensure uniqueness across all searches
	selectOptions := make([]discordgo.SelectMenuOption, 0, len(results))
	for idx, result := range results {
		titleWithNumber := fmt.Sprintf("%d. %s", idx+1, result.Title)
		// Truncate label if too long (max 100 chars)
		label := titleWithNumber
		if len(label) > 100 {
			label = titleWithNumber[:97] + "..."
		}

		selectOptions = append(selectOptions, discordgo.SelectMenuOption{
			Label:       label,
			Description: result.Duration,
			Value:       fmt.Sprintf("%s:%d", searchMessageID, idx), // Use colon as delimiter!
		})
	}

	// Create embed with search results
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       messages.T().Queue.SearchResultsTitle,
		Description: fmt.Sprintf(messages.T().Queue.SearchResultsDesc, query),
		Fields:      make([]*discordgo.MessageEmbedField, 0, len(results)),
	}

	for idx, result := range results {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%d. %s", idx+1, result.Title),
			Value:  result.Duration,
			Inline: false,
		})
	}

	// Create select menu with unique ID per search
	customID := fmt.Sprintf("search_select_%s", searchMessageID)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    customID,
					Placeholder: messages.T().Queue.SelectPlaceholder,
					Options:     selectOptions,
				},
			},
		},
	}

	// Update message with results
	if err := UpdateResponseEmbedWithComponents(s, i, embed, components); err != nil {
		logger.Errorf("[Search] Failed to update message with results: %v", err)
		return err
	}

	// Handle select menu interaction with timeout
	go handleSearchSelection(s, i, results, customID, voiceState.ChannelID, searchMessageID)

	return nil
}

// handleSearchSelection handles the select menu interaction for search results
func handleSearchSelection(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, results []youtube.SearchResult, customID, voiceChannelID, searchMessageID string) {
	logger.Debugf("[Search] handleSearchSelection started, customID='%s', searchMessageID='%s'", customID, searchMessageID)
	timeout := time.After(30 * time.Second)

	// Cleanup function to remove from map after timeout
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

		// Only allow the original requester to select
		if i.Member.User.ID != originalInteraction.Member.User.ID {
			embed := messages.CreateWarningEmbed(messages.TitleNoPermission, messages.T().Queue.OnlyRequester)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{embed},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Parse value: format is "{searchMessageID}:{index}"
		if len(data.Values) == 0 {
			logger.Warnf("[Search] No value in selection data")
			return
		}

		logger.Debugf("[Search] Parsing value: '%s'", data.Values[0])

		// Split value into searchMessageID and index (using colon as delimiter)
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

		// CRITICAL: Verify this selection belongs to THIS search
		if valueSearchID != searchMessageID {
			logger.Debugf("[Search] Value searchID mismatch: expected %s, got %s - ignoring", searchMessageID, valueSearchID)
			return
		}

		// Try to claim selection using global map (prevents duplicates per message)
		searchSelectionsMu.Lock()
		logger.Debugf("[Search] Checking duplicate map with searchMessageID='%s', already_selected=%v", searchMessageID, searchSelections[searchMessageID])
		if searchSelections[searchMessageID] {
			// Already selected - ignore duplicate
			searchSelectionsMu.Unlock()
			logger.Debugf("[Search] Duplicate selection attempt ignored for search message: '%s'", searchMessageID)
			embed := messages.CreateWarningEmbed(messages.T().Queue.AlreadySelectedTitle, messages.T().Queue.AlreadySelectedDesc)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{embed},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
		// Mark as selected
		logger.Debugf("[Search] Marking search as selected: searchMessageID='%s'", searchMessageID)
		searchSelections[searchMessageID] = true
		searchSelectionsMu.Unlock()

		// Validate index
		if selectedIndex < 0 || selectedIndex >= len(results) {
			logger.Warnf("[Search] Invalid index %d for search with %d results", selectedIndex, len(results))
			return
		}

		selectedResult := results[selectedIndex]

		// Acknowledge the interaction immediately to prevent timeout
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})

		// Show processing message
		processingEmbed := messages.CreateWarningEmbed(messages.T().Queue.ProcessingTitle,
			fmt.Sprintf(messages.T().Queue.ProcessingDesc, selectedResult.Title))

		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds:     &[]*discordgo.MessageEmbed{processingEmbed},
			Components: &[]discordgo.MessageComponent{},
		})

		// Get detailed info for selected song
		videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", selectedResult.VideoID)
		logger.Debugf("[Search] Fetching detailed info for selected song: %s", selectedResult.Title)
		fetchStartTime := time.Now()

		song, err := youtube.GetVideoInfo(videoURL, originalInteraction.Member.User.Username, originalInteraction.Member.User.ID)
		if err != nil {
			logger.Errorf("[Search] Error fetching detailed info: %v", err)
			errorEmbed := messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.SearchAddError)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
				Components: &[]discordgo.MessageComponent{},
			})
			return
		}

		fetchEndTime := time.Now()
		logger.Debugf("[Search] Detailed info fetched in %dms for: %s, uploader: %s",
			fetchEndTime.Sub(fetchStartTime).Milliseconds(), song.Title, song.Uploader)

		// Create or get queue
		q, err := queue.GetQueue(originalInteraction.GuildID, false)
		if err != nil {
			logger.Errorf("[Search] Error getting queue: %v", err)
			return
		}

		if q == nil {
			if err := queue.CreateQueue(originalInteraction.GuildID, originalInteraction.ChannelID, voiceChannelID); err != nil {
				logger.Errorf("[Search] Error creating queue: %v", err)
				errorEmbed := messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.QueueCreateFailed)
				s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Embeds: &[]*discordgo.MessageEmbed{errorEmbed},
					Components: &[]discordgo.MessageComponent{},
				})
				return
			}
			q, _ = queue.GetQueue(originalInteraction.GuildID, false)
		}

		// Add song to queue
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
			errorEmbed := messages.CreateErrorEmbed(messages.TitleError,
				fmt.Sprintf(messages.T().Music.SongAddFailed, err))
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{errorEmbed},
				Components: &[]discordgo.MessageComponent{},
			})
			return
		}

		// Show success message
		addedEmbed := messages.CreateSongEmbed(
			messages.ColorSuccess,
			messages.TitleAdded,
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

		// Start playing if queue was empty
		q, _ = queue.GetQueue(originalInteraction.GuildID, false)
		p := player.GetPlayer(originalInteraction.GuildID)
		if q != nil && len(q.Songs) == 1 && !p.Playing && !p.Loading {
			go player.Play(s, originalInteraction.GuildID)
		}
	}

	removeHandler := s.AddHandler(selectionHandler)
	defer removeHandler()

	<-timeout

	// Timeout - update message
	timeoutEmbed := messages.CreateWarningEmbed(messages.T().Queue.SearchTimeoutTitle, messages.T().Queue.SearchTimeoutDesc)

	// Edit the original response using webhook edit (works for both command types)
	s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{timeoutEmbed},
		Components: &[]discordgo.MessageComponent{},
	})
}

// HandleSkipTo handles the skipto command
func HandleSkipTo(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.SkipToEnterPosition))
		return nil
	}
	position := int(options[0].IntValue())

	// Check user in voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.EnterVoiceChannel))
		return nil
	}

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleEmptyQueue, messages.DescEmptyQueue))
		return nil
	}

	// Validate position
	if position < 1 || position > len(q.Songs) {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	if position == 1 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.SkipToCurrent))
		return nil
	}

	targetSong := q.Songs[position-1]

	// CRITICAL: Skip operations BEFORE sending message (prevents race condition)
	// Remove all songs before target position
	if err := queue.SkipToPosition(i.GuildID, position-1); err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Queue.SkipToFailed, err)))
		return err
	}

	// Stop current playback and start target song (without removing it)
	p := player.GetPlayer(i.GuildID)
	if p.Playing || p.Loading {
		if err := player.SkipTo(s, i.GuildID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
				fmt.Sprintf(messages.T().Queue.SkipToFailed, err)))
			return err
		}
	}

	// NOW send success message
	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T().Queue.SkipToCompleteTitle,
		Description: fmt.Sprintf(messages.T().Queue.SkipToCompleteDesc,
			messages.EscapeMarkdown(targetSong.Title), targetSong.URL),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T().Queue.SkipToSkippedSongs, Value: fmt.Sprintf(messages.T().Queue.SkipToSongsCount, position-1), Inline: true},
			{Name: messages.FieldRequester, Value: messages.EscapeMarkdown(targetSong.RequestedByTag), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: targetSong.Thumbnail},
	}

	UpdateResponseEmbed(s, i, embed)
	return nil
}

// HandleSwap handles the swap command
func HandleSwap(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options
	if len(options) < 2 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Queue.SwapEnterPositions))
		return nil
	}
	pos1 := int(options[0].IntValue()) - 1 // Convert to 0-based
	pos2 := int(options[1].IntValue()) - 1

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleEmptyQueue, messages.DescEmptyQueue))
		return nil
	}

	// Validate positions
	if pos1 < 0 || pos2 < 0 || pos1 >= len(q.Songs) || pos2 >= len(q.Songs) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Queue.EnterValidRange, len(q.Songs))))
		return nil
	}

	// Prevent swapping currently playing song (position 0)
	if (pos1 == 0 || pos2 == 0) && (q.Playing || q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Queue.CannotSwapCurrent))
		return nil
	}

	// Check ownership - user can only swap their own songs
	if q.Songs[pos1].RequestedByID != i.Member.User.ID || q.Songs[pos2].RequestedByID != i.Member.User.ID {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoPermission,
			messages.T().Queue.OnlyOwnSwap))
		return nil
	}

	// Get song titles for confirmation message
	song1Title := q.Songs[pos1].Title
	song2Title := q.Songs[pos2].Title

	// Swap
	if err := queue.SwapSongs(i.GuildID, pos1, pos2); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Queue.SwapFailed, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color: messages.ColorSuccess,
		Title: messages.T().Queue.SwapCompleteTitle,
		Description: fmt.Sprintf(messages.T().Queue.SwapCompleteDesc,
			pos1+1, messages.EscapeMarkdown(song1Title),
			pos2+1, messages.EscapeMarkdown(song2Title)),
	}
	RespondEmbed(s, i, embed)
	return nil
}

// formatSeconds formats seconds to HH:MM:SS or MM:SS
func formatSeconds(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// boolToOnOff converts bool to "On"/"Off"
func boolToOnOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}
