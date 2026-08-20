package play

import (
	"fmt"
	"noraegaori/internal/discord"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/shutdown"
	"noraegaori/internal/youtube"
	ytdlpUpdater "noraegaori/internal/ytdlp"
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
		discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.PlaylistInfoFailed))
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

	discord.UpdateResponseEmbed(s, i, confirmEmbed)

	msg, err := discord.GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("Failed to get interaction response: %v", err)
		return err
	}

	waitForConfirmation := handlePlaylistConfirmationReaction(s, i, msg, playlistInfo, voiceState)
	go waitForConfirmation()

	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "✅")
	if err != nil {
		logger.Errorf("Failed to add reaction: %v", err)
		return err
	}

	return nil
}

func resolveVideoWithPlaylistFallback(i *discordgo.InteractionCreate, analysis *youtube.URLAnalysis) (*youtube.Song, error) {
	cleanVideoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", analysis.VideoID)
	song, videoErr := youtube.GetVideoInfo(i.GuildID, cleanVideoURL, i.Member.User.Username, i.Member.User.ID)
	if videoErr == nil {
		return song, nil
	}

	logger.Debugf("Direct video fetch failed, trying to get info from playlist")
	playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", analysis.PlaylistID)
	playlistInfo, playlistErr := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
	if playlistErr != nil {
		return nil, videoErr
	}

	for _, video := range playlistInfo.Videos {
		if strings.Contains(video.URL, analysis.VideoID) {
			logger.Debugf("Found video in playlist by ID, using playlist info: %s", video.Title)
			return video, nil
		}
	}

	if len(playlistInfo.Videos) > 0 {
		logger.Debugf("Video ID not in playlist, using first video: %s", playlistInfo.Videos[0].Title)
		return playlistInfo.Videos[0], nil
	}

	return nil, videoErr
}

func videoUnavailableEmbed(guildID string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       messages.T(guildID).Music.VideoUnavailableTitle,
		Description: messages.T(guildID).Music.VideoUnavailableDesc,
		Footer:      &discordgo.MessageEmbedFooter{Text: messages.T(guildID).Music.VideoUnavailableFooter},
	}
}

func videoWithPlaylistEmbed(guildID string, song *youtube.Song, isDuplicate bool) *discordgo.MessageEmbed {
	template := messages.T(guildID).Music.VideoWithPlaylistFound
	if isDuplicate {
		template = messages.T(guildID).Music.VideoWithPlaylistDuplicate
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T(guildID).Titles.Added,
		Description: fmt.Sprintf(template, messages.FormatBoldMaskedLink(song.Title, song.URL)),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(guildID).Fields.Uploader, Value: messages.EscapeMarkdown(song.Uploader), Inline: true},
			{Name: messages.T(guildID).Fields.Duration, Value: song.Duration, Inline: true},
			{Name: messages.T(guildID).Fields.Requester, Value: messages.EscapeMarkdown(song.RequestedBy), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
		Footer:    &discordgo.MessageEmbedFooter{Text: messages.T(guildID).Music.VideoWithPlaylistFooter},
	}
}

func handleVideoWithPlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, videoURL string, analysis *youtube.URLAnalysis, voiceState *discordgo.VoiceState) error {

	song, videoErr := resolveVideoWithPlaylistFallback(i, analysis)
	videoUnavailable := song == nil

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.QueueCreateFailed))
			return err
		}
	}

	isDuplicate := false
	var embed *discordgo.MessageEmbed

	if videoUnavailable {
		logger.Warnf("Specific video unavailable (even from playlist), offering playlist: %v", videoErr)
		embed = videoUnavailableEmbed(i.GuildID)
	} else {
		if err := queue.AddSong(i.GuildID, queueSongFrom(song), -1); err != nil {
			if err.Error() != "song already in queue: "+song.Title {
				discord.UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.SongAddFailed, err)))
				return err
			}
			isDuplicate = true
		}

		embed = videoWithPlaylistEmbed(i.GuildID, song, isDuplicate)
	}

	discord.UpdateResponseEmbed(s, i, embed)

	msg, err := discord.GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("Failed to get interaction response: %v", err)
		return err
	}

	excludeVideoID := analysis.VideoID
	if videoUnavailable {
		excludeVideoID = ""
	}
	waitForConfirmation := handlePlaylistRestConfirmationReaction(s, i, msg, analysis.PlaylistID, excludeVideoID, voiceState)
	go waitForConfirmation()

	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "⬇️")
	if err != nil {
		logger.Errorf("Failed to add reaction: %v", err)
		return err
	}

	if !videoUnavailable && !isDuplicate {
		startPlaybackIfFirstSong(s, i.GuildID)
	}

	return nil
}

func editPromptEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, msg *discordgo.Message, embed *discordgo.MessageEmbed) {
	if discord.IsMessageCommand(i) {
		s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, embed)
		return
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func handlePlaylistConfirmationReaction(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistInfo *youtube.PlaylistInfo, voiceState *discordgo.VoiceState) func() {
	logger.Debugf("Starting reaction handler for message %s in channel %s", msg.ID, msg.ChannelID)
	logger.Debugf("Expecting reaction from user: %s", originalInteraction.Member.User.ID)

	confirmedChan := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		logger.Debugf("Received reaction: emoji=%s, messageID=%s, userID=%s", r.Emoji.Name, r.MessageID, r.UserID)

		if r.UserID == s.State.User.ID {
			logger.Debugf("Ignoring bot's own reaction")
			return
		}

		if r.MessageID != msg.ID {
			logger.Debugf("Message ID mismatch: expected %s, got %s", msg.ID, r.MessageID)
			return
		}

		if r.Emoji.Name != "✅" {
			logger.Debugf("Emoji mismatch: expected ✅, got %s", r.Emoji.Name)
			return
		}
		if r.UserID != originalInteraction.Member.User.ID {
			logger.Debugf("User ID mismatch: expected %s, got %s", originalInteraction.Member.User.ID, r.UserID)
			discord.RemoveUserReaction(s, msg.ChannelID, msg.ID, r.Emoji.Name, r.UserID)
			return
		}

		logger.Debugf("Confirmed by user %s", r.UserID)

		select {
		case confirmedChan <- true:
		default:
		}

		loadingEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistAddingTitle, messages.T(originalInteraction.GuildID).Music.PlaylistAddingAll)

		editPromptEmbed(s, originalInteraction, msg, loadingEmbed)

		discord.ClearPromptReactions(s, msg.ChannelID, msg.ID)

		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceState, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)

	return func() {
		defer removeHandler()

		select {
		case <-confirmedChan:
			logger.Debugf("Reaction confirmed, handler complete")
		case <-time.After(30 * time.Second):
			logger.Debugf("Timeout reached, cancelling")
			embed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutTitle, messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutDesc)
			editPromptEmbed(s, originalInteraction, msg, embed)
			discord.ClearPromptReactions(s, msg.ChannelID, msg.ID)
		}
	}
}

func confirmedByRequester(s *discordgo.Session, r *discordgo.MessageReactionAdd, msg *discordgo.Message, originalInteraction *discordgo.InteractionCreate) bool {
	logger.Debugf("Received reaction: emoji=%s, messageID=%s, userID=%s", r.Emoji.Name, r.MessageID, r.UserID)

	if r.UserID == s.State.User.ID || r.MessageID != msg.ID || r.Emoji.Name != "⬇️" {
		return false
	}

	if r.UserID != originalInteraction.Member.User.ID {
		logger.Debugf("User ID mismatch: expected %s, got %s", originalInteraction.Member.User.ID, r.UserID)
		discord.RemoveUserReaction(s, msg.ChannelID, msg.ID, r.Emoji.Name, r.UserID)
		return false
	}

	logger.Debugf("Confirmed by user %s", r.UserID)
	return true
}

func fetchPlaylistExcluding(i *discordgo.InteractionCreate, playlistID, videoID string) (*youtube.PlaylistInfo, error) {
	playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", playlistID)
	playlistInfo, err := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		return nil, err
	}

	playlistInfo.Videos = excludeVideo(playlistInfo.Videos, videoID)

	return playlistInfo, nil
}

func excludeVideo(videos []*youtube.Song, videoID string) []*youtube.Song {
	if videoID == "" {
		return videos
	}

	remaining := make([]*youtube.Song, 0, len(videos))
	for _, video := range videos {
		if !strings.Contains(video.URL, videoID) {
			remaining = append(remaining, video)
		}
	}
	return remaining
}

func handlePlaylistRestConfirmationReaction(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistID, videoID string, voiceState *discordgo.VoiceState) func() {
	logger.Debugf("Starting reaction handler for message %s in channel %s", msg.ID, msg.ChannelID)
	logger.Debugf("Expecting reaction from user: %s", originalInteraction.Member.User.ID)

	confirmedChan := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if !confirmedByRequester(s, r, msg, originalInteraction) {
			return
		}

		select {
		case confirmedChan <- true:
		default:
		}

		playlistInfo, err := fetchPlaylistExcluding(originalInteraction, playlistID, videoID)
		if err != nil {
			errorEmbed := messages.CreateErrorEmbed(messages.T(originalInteraction.GuildID).Titles.Error, messages.T(originalInteraction.GuildID).Music.PlaylistInfoFailed)
			editPromptEmbed(s, originalInteraction, msg, errorEmbed)
			discord.ClearPromptReactions(s, msg.ChannelID, msg.ID)
			return
		}

		loadingEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistAddingTitle, messages.T(originalInteraction.GuildID).Music.PlaylistAddingRest)

		editPromptEmbed(s, originalInteraction, msg, loadingEmbed)

		discord.ClearPromptReactions(s, msg.ChannelID, msg.ID)

		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceState, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)

	return func() {
		defer removeHandler()

		select {
		case <-confirmedChan:
			logger.Debugf("Reaction confirmed, handler complete")
		case <-time.After(30 * time.Second):
			logger.Debugf("Timeout reached, cancelling")
			embed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutTitle, messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutDesc)
			editPromptEmbed(s, originalInteraction, msg, embed)
			discord.ClearPromptReactions(s, msg.ChannelID, msg.ID)
		}
	}
}

func addPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, playlistInfo *youtube.PlaylistInfo, voiceState *discordgo.VoiceState, messageID string) {
	lock := getPlaylistLock(i.GuildID)
	lock.Lock()
	defer lock.Unlock()

	startTime := time.Now()
	logger.Debugf("Starting playlist processing for %d songs", len(playlistInfo.Videos))

	q, _ := queue.GetQueue(i.GuildID, false)
	isQueueEmpty := q == nil || len(q.Songs) == 0

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			logger.Errorf("Failed to create queue: %v", err)
			return
		}
	} else {
		if err := queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID); err != nil {
			logger.Errorf("Failed to update voice channel for %s: %v", i.GuildID, err)
		}
	}

	if isQueueEmpty && len(playlistInfo.Videos) > 0 {
		logger.Debug("Fast-tracking first song for immediate playback")

		addedCount, skippedCount := fastTrackFirstSong(i.GuildID, playlistInfo.Videos, s, i)

		initialTime := time.Since(startTime)
		logger.Debugf("First song processed in %dms: %d added, %d skipped",
			initialTime.Milliseconds(), addedCount, skippedCount)

		if addedCount > 0 {
			go player.Play(s, i.GuildID)
		}

		if len(playlistInfo.Videos) > 1 && addedCount > 0 {
			remainingSongs := playlistInfo.Videos[1:]
			logger.Debugf("Processing remaining %d songs (synchronously to maintain order)", len(remainingSongs))
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
			logger.Debugf("Skipping unavailable video: %s - %v", song.Title, err)
			skippedCount++
			continue
		}

		if isLive {
			song.IsLive = true
			song.Duration = "🔴 LIVE"
		}

		queueSong := queueSongFrom(song)

		if err := queue.AddSong(guildID, queueSong, -1); err != nil {
			if strings.Contains(err.Error(), "already in queue") {
				logger.Debugf("Skipping duplicate: %s", song.Title)
				skippedCount++
				continue
			}
			logger.Errorf("Error adding first song: %v", err)
			skippedCount++
			continue
		}

		addedCount = 1
		logger.Debugf("First song added: %s", song.Title)
		break
	}

	return addedCount, skippedCount
}

func processRemainingPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Debugf("Processing %d remaining songs with worker pool", len(songs))

	workerPool := youtube.GetAvailabilityPool()

	jobs := make([]youtube.BatchJob, 0, len(songs))
	for idx, song := range songs {
		jobs = append(jobs, youtube.BatchJob{
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
			logger.Debugf("Skipping definitively unavailable: %s - %s",
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

		queueSong := queueSongFrom(song)

		if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
			if strings.Contains(err.Error(), "already in queue") {
				logger.Debugf("Skipping duplicate: %s", song.Title)
				skippedCount++
			} else {
				logger.Errorf("Error adding song: %v", err)
				skippedCount++
			}
			continue
		}

		addedCount++
		logger.Debugf("Added song %d/%d: %s", addedCount, len(songs), song.Title)
	}

	totalTime := time.Since(startTime)
	logger.Debugf("Completed: %d added, %d skipped in %dms total",
		addedCount, skippedCount, totalTime.Milliseconds())

	if shutdown.IsShuttingDown() {
		logger.Debug("Skipping completion message - bot is shutting down")
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
		logger.Errorf("Failed to update completion message: %v", err)
	}

	sendBatchedSkipNotice(s, i.GuildID, i.ChannelID, skippedSongs)
}

func processAllPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Debugf("Standard processing for %d songs", len(songs))

	workerPool := youtube.GetAvailabilityPool()

	jobs := make([]youtube.BatchJob, 0, len(songs))
	for idx, song := range songs {
		jobs = append(jobs, youtube.BatchJob{
			URL:   song.URL,
			Index: idx,
		})
	}

	results := workerPool.CheckBatch(jobs)
	checkTime := time.Since(startTime)
	logger.Debugf("Availability check completed in %dms", checkTime.Milliseconds())

	addedCount := 0
	skippedCount := 0
	var skippedSongs []skippedSong

	for _, result := range results {
		song := songs[result.Index]

		if !result.Available && ytdlpUpdater.IsDefinitiveUnavailableError(result.Error) {
			logger.Debugf("Skipping definitively unavailable: %s - %s",
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

		queueSong := queueSongFrom(song)

		if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
			if strings.Contains(err.Error(), "already in queue") {
				logger.Debugf("Skipping duplicate: %s", song.Title)
				skippedCount++
			} else {
				logger.Errorf("Error adding song: %v", err)
				skippedCount++
			}
			continue
		}

		addedCount++
	}

	totalTime := time.Since(startTime)
	logger.Debugf("Completed: %d added, %d skipped in %dms", addedCount, skippedCount, totalTime.Milliseconds())

	if shutdown.IsShuttingDown() {
		logger.Debug("Skipping completion message - bot is shutting down")
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
		logger.Errorf("Failed to update completion message: %v", err)
	}

	sendBatchedSkipNotice(s, i.GuildID, i.ChannelID, skippedSongs)

	q, _ := queue.GetQueue(i.GuildID, true)
	if q == nil || len(q.Songs) == 0 {
		return
	}
	resumeOrStartPlayback(s, i.GuildID)
}
