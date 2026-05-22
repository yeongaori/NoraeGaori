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
	"noraegaori/internal/shutdown"
	"noraegaori/internal/worker"
	"noraegaori/internal/youtube"
	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"
)

var (
	playlistLocks   = make(map[string]*sync.Mutex)
	playlistLocksMu sync.Mutex
)

func getPlaylistLock(guildID string) *sync.Mutex {
	playlistLocksMu.Lock()
	defer playlistLocksMu.Unlock()

	if _, exists := playlistLocks[guildID]; !exists {
		playlistLocks[guildID] = &sync.Mutex{}
	}
	return playlistLocks[guildID]
}

func handlePurePlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, playlistURL string, voiceState *discordgo.VoiceState) error {
	playlistInfo, err := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.PlaylistInfoFailed))
		return err
	}

	confirmEmbed := &discordgo.MessageEmbed{
		Color: messages.ColorInfo,
		Title: messages.T(i.GuildID).Titles.PlaylistFound,
		Description: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistConfirmDesc,
			messages.FormatBoldMaskedLink(playlistInfo.Title, playlistInfo.URL), playlistInfo.VideoCount),
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
		Footer:    &discordgo.MessageEmbedFooter{Text: messages.T(i.GuildID).Music.PlaylistConfirmFooter},
	}

	UpdateResponseEmbed(s, i, confirmEmbed)

	msg, err := GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("[Playlist] Failed to get interaction response: %v", err)
		return err
	}

	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "✅")
	if err != nil {
		logger.Errorf("[Playlist] Failed to add reaction: %v", err)
		return err
	}

	go handlePlaylistConfirmationReaction(s, i, msg, playlistInfo, voiceState)

	return nil
}

func handleVideoWithPlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, videoURL string, analysis *youtube.URLAnalysis, voiceState *discordgo.VoiceState) error {
	
	cleanVideoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", analysis.VideoID)
	song, videoErr := youtube.GetVideoInfo(i.GuildID, cleanVideoURL, i.Member.User.Username, i.Member.User.ID)
	videoUnavailable := videoErr != nil

	
	if videoUnavailable {
		logger.Debugf("[Play] Direct video fetch failed, trying to get info from playlist")
		playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", analysis.PlaylistID)
		playlistInfo, playlistErr := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
		if playlistErr == nil {
			for _, video := range playlistInfo.Videos {
				if strings.Contains(video.URL, analysis.VideoID) {
					logger.Debugf("[Play] Found video in playlist by ID, using playlist info: %s", video.Title)
					song = video
					videoUnavailable = false
					break
				}
			}
			
			if videoUnavailable && len(playlistInfo.Videos) > 0 {
				song = playlistInfo.Videos[0]
				logger.Debugf("[Play] Video ID not in playlist, using first video: %s", song.Title)
				videoUnavailable = false
			}
		}
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed))
			return err
		}
	}

	isDuplicate := false
	var embed *discordgo.MessageEmbed

	if videoUnavailable {
		logger.Warnf("[Play] Specific video unavailable (even from playlist), offering playlist: %v", videoErr)

		embed = &discordgo.MessageEmbed{
			Color:       messages.ColorWarning,
			Title:       messages.T(i.GuildID).Music.VideoUnavailableTitle,
			Description: messages.T(i.GuildID).Music.VideoUnavailableDesc,
			Footer:      &discordgo.MessageEmbedFooter{Text: messages.T(i.GuildID).Music.VideoUnavailableFooter},
		}
	} else {
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

		if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
			if err.Error() == "song already in queue: "+song.Title {
				isDuplicate = true
			} else {
				UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err)))
				return err
			}
		}

		var description string
		if isDuplicate {
			description = fmt.Sprintf(messages.T(i.GuildID).Music.VideoWithPlaylistDuplicate,
				messages.FormatBoldMaskedLink(song.Title, song.URL))
		} else {
			description = fmt.Sprintf(messages.T(i.GuildID).Music.VideoWithPlaylistFound,
				messages.FormatBoldMaskedLink(song.Title, song.URL))
		}

		embed = &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       messages.T(i.GuildID).Titles.Added,
			Description: description,
			Fields: []*discordgo.MessageEmbedField{
				{Name: messages.T(i.GuildID).Fields.Uploader, Value: messages.EscapeMarkdown(song.Uploader), Inline: true},
				{Name: messages.T(i.GuildID).Fields.Duration, Value: song.Duration, Inline: true},
				{Name: messages.T(i.GuildID).Fields.Requester, Value: messages.EscapeMarkdown(song.RequestedBy), Inline: true},
			},
			Thumbnail: &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
			Footer:    &discordgo.MessageEmbedFooter{Text: messages.T(i.GuildID).Music.VideoWithPlaylistFooter},
		}
	}

	UpdateResponseEmbed(s, i, embed)

	msg, err := GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("[Play] Failed to get interaction response: %v", err)
		return err
	}

	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "⬇️")
	if err != nil {
		logger.Errorf("[Play] Failed to add reaction: %v", err)
		return err
	}

	if !videoUnavailable && !isDuplicate {
		q, _ = queue.GetQueue(i.GuildID, true)
		p := player.GetPlayer(i.GuildID)
		if len(q.Songs) == 1 && !p.Playing && !p.Loading {
			go player.Play(s, i.GuildID)
		}
	}

	
	excludeVideoID := analysis.VideoID
	if videoUnavailable {
		excludeVideoID = ""
	}
	go handlePlaylistRestConfirmationReaction(s, i, msg, analysis.PlaylistID, excludeVideoID, voiceState)

	return nil
}

func handlePlaylistConfirmationReaction(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistInfo *youtube.PlaylistInfo, voiceState *discordgo.VoiceState) {
	logger.Debugf("[PlaylistReaction] Starting reaction handler for message %s in channel %s", msg.ID, msg.ChannelID)
	logger.Debugf("[PlaylistReaction] Expecting reaction from user: %s", originalInteraction.Member.User.ID)

	confirmedChan := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		logger.Debugf("[PlaylistReaction] Received reaction: emoji=%s, messageID=%s, userID=%s", r.Emoji.Name, r.MessageID, r.UserID)

		if r.UserID == s.State.User.ID {
			logger.Debugf("[PlaylistReaction] Ignoring bot's own reaction")
			return
		}

		if r.MessageID != msg.ID {
			logger.Debugf("[PlaylistReaction] Message ID mismatch: expected %s, got %s", msg.ID, r.MessageID)
			return
		}

		if r.Emoji.Name != "✅" {
			logger.Debugf("[PlaylistReaction] Emoji mismatch: expected ✅, got %s", r.Emoji.Name)
			return
		}
		if r.UserID != originalInteraction.Member.User.ID {
			logger.Debugf("[PlaylistReaction] User ID mismatch: expected %s, got %s", originalInteraction.Member.User.ID, r.UserID)
			return
		}

		logger.Debugf("[PlaylistReaction] Confirmed by user %s", r.UserID)

		select { 
		case confirmedChan <- true:
		default:
		}

		loadingEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistAddingTitle, messages.T(originalInteraction.GuildID).Music.PlaylistAddingAll)

		
		if isMessageCommand(originalInteraction) {
			s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, loadingEmbed)
		} else {
			s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
				Embeds: &[]*discordgo.MessageEmbed{loadingEmbed},
			})
		}

		s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)

		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceState, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)
	defer removeHandler()

	select {
	case <-confirmedChan:
		logger.Debugf("[PlaylistReaction] Reaction confirmed, handler complete")
	case <-time.After(30 * time.Second):
		logger.Debugf("[PlaylistReaction] Timeout reached, cancelling")
		embed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutTitle, messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutDesc)
		if isMessageCommand(originalInteraction) {
			s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, embed)
		} else {
			s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
				Embeds: &[]*discordgo.MessageEmbed{embed},
			})
		}
		s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)
	}
}

func handlePlaylistRestConfirmationReaction(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistID, videoID string, voiceState *discordgo.VoiceState) {
	logger.Debugf("[PlaylistRestReaction] Starting reaction handler for message %s in channel %s", msg.ID, msg.ChannelID)
	logger.Debugf("[PlaylistRestReaction] Expecting reaction from user: %s", originalInteraction.Member.User.ID)

	confirmedChan := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		logger.Debugf("[PlaylistRestReaction] Received reaction: emoji=%s, messageID=%s, userID=%s", r.Emoji.Name, r.MessageID, r.UserID)

		if r.UserID == s.State.User.ID {
			logger.Debugf("[PlaylistRestReaction] Ignoring bot's own reaction")
			return
		}

		if r.MessageID != msg.ID {
			logger.Debugf("[PlaylistRestReaction] Message ID mismatch: expected %s, got %s", msg.ID, r.MessageID)
			return
		}

		if r.Emoji.Name != "⬇️" {
			logger.Debugf("[PlaylistRestReaction] Emoji mismatch: expected ⬇️, got %s", r.Emoji.Name)
			return
		}

		if r.UserID != originalInteraction.Member.User.ID {
			logger.Debugf("[PlaylistRestReaction] User ID mismatch: expected %s, got %s", originalInteraction.Member.User.ID, r.UserID)
			return
		}

		logger.Debugf("[PlaylistRestReaction] Confirmed by user %s", r.UserID)

		select {
		case confirmedChan <- true:
		default:
		}

		playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", playlistID)
		playlistInfo, err := youtube.GetPlaylistInfo(playlistURL, originalInteraction.Member.User.Username, originalInteraction.Member.User.ID)
		if err != nil {
			errorEmbed := messages.CreateErrorEmbed(messages.T(originalInteraction.GuildID).Titles.Error, messages.T(originalInteraction.GuildID).Music.PlaylistInfoFailed)
			if isMessageCommand(originalInteraction) {
				s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, errorEmbed)
			} else {
				s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
					Embeds: &[]*discordgo.MessageEmbed{errorEmbed},
				})
			}
			s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)
			return
		}

		if videoID != "" {
			filteredVideos := make([]*youtube.Song, 0)
			for _, video := range playlistInfo.Videos {
				if !strings.Contains(video.URL, videoID) {
					filteredVideos = append(filteredVideos, video)
				}
			}
			playlistInfo.Videos = filteredVideos
		}

		loadingEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistAddingTitle, messages.T(originalInteraction.GuildID).Music.PlaylistAddingRest)

		if isMessageCommand(originalInteraction) {
			s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, loadingEmbed)
		} else {
			s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
				Embeds: &[]*discordgo.MessageEmbed{loadingEmbed},
			})
		}

		s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)

		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceState, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)
	defer removeHandler()

	select {
	case <-confirmedChan:
		logger.Debugf("[PlaylistRestReaction] Reaction confirmed, handler complete")
	case <-time.After(30 * time.Second):
		logger.Debugf("[PlaylistRestReaction] Timeout reached, cancelling")
		embed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutTitle, messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutDesc)
		if isMessageCommand(originalInteraction) {
			s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, embed)
		} else {
			s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
				Embeds: &[]*discordgo.MessageEmbed{embed},
			})
		}
		s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)
	}
}

func addPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, playlistInfo *youtube.PlaylistInfo, voiceState *discordgo.VoiceState, messageID string) {
	lock := getPlaylistLock(i.GuildID)
	lock.Lock()
	defer lock.Unlock()

	startTime := time.Now()
	logger.Debugf("[Playlist] Starting playlist processing for %d songs", len(playlistInfo.Videos))

	q, _ := queue.GetQueue(i.GuildID, false)
	isQueueEmpty := q == nil || len(q.Songs) == 0

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			logger.Errorf("[Playlist] Failed to create queue: %v", err)
			return
		}
	} else {
		queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID)
	}

	if isQueueEmpty && len(playlistInfo.Videos) > 0 {
		logger.Debug("[Playlist] Fast-tracking first song for immediate playback")

		addedCount, skippedCount := fastTrackFirstSong(i.GuildID, playlistInfo.Videos, s, i)

		initialTime := time.Since(startTime)
		logger.Debugf("[Playlist] First song processed in %dms: %d added, %d skipped",
			initialTime.Milliseconds(), addedCount, skippedCount)

		if addedCount > 0 {
			go player.Play(s, i.GuildID)
		}

		if len(playlistInfo.Videos) > 1 && addedCount > 0 {
			remainingSongs := playlistInfo.Videos[1:]
			logger.Debugf("[Playlist] Processing remaining %d songs (synchronously to maintain order)", len(remainingSongs))
			processRemainingPlaylistSongs(s, i, remainingSongs, playlistInfo, startTime, messageID)
		}

		return
	}

	processAllPlaylistSongs(s, i, playlistInfo.Videos, playlistInfo, startTime, messageID)
}

func fastTrackFirstSong(guildID string, songs []*youtube.Song, s *discordgo.Session, i *discordgo.InteractionCreate) (addedCount, skippedCount int) {
	maxAttempts := 3
	if len(songs) < maxAttempts {
		maxAttempts = len(songs)
	}

	for idx := 0; idx < maxAttempts; idx++ {
		song := songs[idx]

		available, isLive, err := youtube.CheckAvailability(song.URL)
		if err != nil || !available {
			logger.Debugf("[Playlist] Skipping unavailable video: %s - %v", song.Title, err)
			skippedCount++
			continue
		}

		if isLive {
			song.IsLive = true
			song.Duration = "🔴 LIVE"
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

		if err := queue.AddSong(guildID, queueSong, -1); err != nil {
			if strings.Contains(err.Error(), "already in queue") {
				logger.Debugf("[Playlist] Skipping duplicate: %s", song.Title)
				skippedCount++
				continue
			}
			logger.Errorf("[Playlist] Error adding first song: %v", err)
			skippedCount++
			continue
		}

		addedCount = 1
		logger.Debugf("[Playlist] First song added: %s", song.Title)
		break
	}

	return addedCount, skippedCount
}

func processRemainingPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Debugf("[Playlist Background] Processing %d remaining songs with worker pool", len(songs))

	workerPool := worker.GetWorkerPool()

	jobs := make([]worker.AvailabilityJob, 0, len(songs))
	for idx, song := range songs {
		jobs = append(jobs, worker.AvailabilityJob{
			URL:   song.URL,
			Index: idx,
		})
	}

	results := workerPool.CheckBatch(jobs)

	addedCount := 0
	skippedCount := 0
	var skippedSongs []skippedSong

	for _, result := range results {
		song := songs[result.Index]

		
		if !result.Available && ytdlpUpdater.IsDefinitiveUnavailableError(result.Error) {
			logger.Debugf("[Playlist Background] Skipping definitively unavailable: %s - %s",
				song.Title, result.Error)
			skippedCount++
			skippedSongs = append(skippedSongs, skippedSong{
				Title: song.Title, URL: song.URL, Thumbnail: song.Thumbnail, Error: result.Error,
			})
			continue
		}

		if result.IsLive {
			song.IsLive = true
			song.Duration = "🔴 LIVE"
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

		if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
			if strings.Contains(err.Error(), "already in queue") {
				logger.Debugf("[Playlist Background] Skipping duplicate: %s", song.Title)
				skippedCount++
			} else {
				logger.Errorf("[Playlist Background] Error adding song: %v", err)
				skippedCount++
			}
			continue
		}

		addedCount++
		logger.Debugf("[Playlist Background] Added song %d/%d: %s", addedCount, len(songs), song.Title)
	}

	totalTime := time.Since(startTime)
	logger.Debugf("[Playlist Background] Completed: %d added, %d skipped in %dms total",
		addedCount, skippedCount, totalTime.Milliseconds())

	if shutdown.IsShuttingDown() {
		logger.Debug("[Playlist Background] Skipping completion message - bot is shutting down")
		return
	}

	description := fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistCompleteDesc,
		messages.FormatBoldMaskedLink(playlistInfo.Title, playlistInfo.URL))

	if skippedCount > 0 {
		description += fmt.Sprintf("\n\n"+messages.T(i.GuildID).Music.PlaylistSkippedCount, skippedCount)
	}

	successEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T(i.GuildID).Titles.PlaylistAdded,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Fields.TotalSongs, Value: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistSongsUnit, playlistInfo.VideoCount), Inline: true},
			{Name: messages.T(i.GuildID).Music.PlaylistAddedCount, Value: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistSongsUnit, addedCount+1), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: i.Member.User.Username, Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
	}

	
	var err error
	if messageID != "" {
		_, err = s.ChannelMessageEditEmbed(i.ChannelID, messageID, successEmbed)
	} else {
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{successEmbed},
		})
	}
	if err != nil {
		logger.Errorf("[Playlist Background] Failed to update completion message: %v", err)
	}

	sendBatchedSkipNotice(s, i.GuildID, i.ChannelID, skippedSongs)
}

func processAllPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Debugf("[Playlist] Standard processing for %d songs", len(songs))

	workerPool := worker.GetWorkerPool()

	jobs := make([]worker.AvailabilityJob, 0, len(songs))
	for idx, song := range songs {
		jobs = append(jobs, worker.AvailabilityJob{
			URL:   song.URL,
			Index: idx,
		})
	}

	results := workerPool.CheckBatch(jobs)
	checkTime := time.Since(startTime)
	logger.Debugf("[Playlist] Availability check completed in %dms", checkTime.Milliseconds())

	addedCount := 0
	skippedCount := 0
	var skippedSongs []skippedSong

	for _, result := range results {
		song := songs[result.Index]

		
		if !result.Available && ytdlpUpdater.IsDefinitiveUnavailableError(result.Error) {
			logger.Debugf("[Playlist] Skipping definitively unavailable: %s - %s",
				song.Title, result.Error)
			skippedCount++
			skippedSongs = append(skippedSongs, skippedSong{
				Title: song.Title, URL: song.URL, Thumbnail: song.Thumbnail, Error: result.Error,
			})
			continue
		}

		if result.IsLive {
			song.IsLive = true
			song.Duration = "🔴 LIVE"
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

		if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
			if strings.Contains(err.Error(), "already in queue") {
				logger.Debugf("[Playlist] Skipping duplicate: %s", song.Title)
				skippedCount++
			} else {
				logger.Errorf("[Playlist] Error adding song: %v", err)
				skippedCount++
			}
			continue
		}

		addedCount++
	}

	totalTime := time.Since(startTime)
	logger.Debugf("[Playlist] Completed: %d added, %d skipped in %dms", addedCount, skippedCount, totalTime.Milliseconds())

	if shutdown.IsShuttingDown() {
		logger.Debug("[Playlist] Skipping completion message - bot is shutting down")
		return
	}

	description := messages.FormatBoldMaskedLink(playlistInfo.Title, playlistInfo.URL)
	if skippedCount > 0 {
		description += fmt.Sprintf("\n\n"+messages.T(i.GuildID).Music.PlaylistSkippedOrDup, skippedCount)
	}

	successEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T(i.GuildID).Titles.PlaylistAdded,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Music.PlaylistAddedSongs, Value: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistSongsUnit, addedCount), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: i.Member.User.Username, Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
	}

	
	var err error
	if messageID != "" {
		_, err = s.ChannelMessageEditEmbed(i.ChannelID, messageID, successEmbed)
	} else {
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Embeds: &[]*discordgo.MessageEmbed{successEmbed},
		})
	}
	if err != nil {
		logger.Errorf("[Playlist] Failed to update completion message: %v", err)
	}

	sendBatchedSkipNotice(s, i.GuildID, i.ChannelID, skippedSongs)

	
	p := player.GetPlayer(i.GuildID)
	q, _ := queue.GetQueue(i.GuildID, true)
	if q == nil || len(q.Songs) == 0 {
		return
	}
	switch {
	case p.Paused:
		logger.Debugf("[Playlist] Resuming playback after playlist addition")
		go player.Resume(s, i.GuildID)
	case !p.Playing && !p.Loading:
		logger.Debugf("[Playlist] Starting playback after playlist addition")
		go player.Play(s, i.GuildID)
	}
}
