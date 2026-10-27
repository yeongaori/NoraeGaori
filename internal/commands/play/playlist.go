package play

import (
	"errors"
	"fmt"
	"noraegaori/internal/discord"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/lockmap"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/shutdown"
	"noraegaori/internal/youtube"
	ytdlpUpdater "noraegaori/internal/ytdlp"
)

var playlistLocks lockmap.Map

var playlistPromptExpiry = 30 * time.Second

func handlePurePlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, playlistURL string, voiceChannelID string) error {
	playlistInfo, err := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Music.PlaylistInfoFailed)
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

	_ = discord.UpdateResponseEmbed(s, i, confirmEmbed)

	msg, err := discord.GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("Failed to get interaction response: %v", err)
		return err
	}

	waitForConfirmation := confirmPlaylist(s, i, msg, playlistInfo, voiceChannelID)
	go waitForConfirmation()

	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "✅")
	if err != nil {
		logger.Errorf("Failed to add reaction: %v", err)
		return err
	}

	return nil
}

func resolvePlaylistVideo(i *discordgo.InteractionCreate, analysis *youtube.URLAnalysis) (*youtube.Song, error) {
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

func createUnavailableEmbed(guildID string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       messages.T(guildID).Music.VideoUnavailableTitle,
		Description: messages.T(guildID).Music.VideoUnavailableDesc,
		Footer:      &discordgo.MessageEmbedFooter{Text: messages.T(guildID).Music.VideoUnavailableFooter},
	}
}

func createPlaylistVideoEmbed(guildID string, song *youtube.Song, isDuplicate bool) *discordgo.MessageEmbed {
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

func playlistTimeoutEmbed(guildID string, song *youtube.Song, isDuplicate bool) *discordgo.MessageEmbed {
	if song == nil {
		return messages.CreateWarningEmbed(messages.T(guildID).Music.PlaylistTimeoutTitle, messages.T(guildID).Music.PlaylistTimeoutDesc)
	}

	color, title, description := messages.ColorSuccess, messages.T(guildID).Titles.Added, ""
	if isDuplicate {
		color, title, description = messages.ColorWarning, messages.T(guildID).Titles.Duplicate, messages.T(guildID).Errors.DuplicateSong
	}

	embed := youtubeSongEmbed(guildID, color, title, description, song)
	embed.Footer = &discordgo.MessageEmbedFooter{Text: messages.T(guildID).Music.PlaylistTimeoutDesc}
	return embed
}

func handlePlaylistVideo(s *discordgo.Session, i *discordgo.InteractionCreate, videoURL string, analysis *youtube.URLAnalysis, voiceChannelID string) error {

	song, videoErr := resolvePlaylistVideo(i, analysis)
	videoUnavailable := song == nil

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err = queue.CreateQueue(i.GuildID, i.ChannelID, voiceChannelID); err != nil {
			discord.UpdateErrorEmbed(s, i, messages.T(i.GuildID).Music.QueueCreateFailed)
			return err
		}
	}

	isDuplicate := false
	var embed *discordgo.MessageEmbed

	if videoUnavailable {
		logger.Warnf("Specific video unavailable (even from playlist), offering playlist: %v", videoErr)
		embed = createUnavailableEmbed(i.GuildID)
	} else {
		embed, isDuplicate, err = addVideoFromPlaylist(s, i, song)
		if err != nil {
			return err
		}
	}

	_ = discord.UpdateResponseEmbed(s, i, embed)

	msg, err := discord.GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("Failed to get interaction response: %v", err)
		return err
	}

	excludeVideoID := analysis.VideoID
	if videoUnavailable {
		excludeVideoID = ""
	}
	timeoutEmbed := playlistTimeoutEmbed(i.GuildID, song, isDuplicate)
	waitForConfirmation := confirmPlaylistRest(s, i, msg, analysis.PlaylistID, excludeVideoID, voiceChannelID, timeoutEmbed)
	go waitForConfirmation()

	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "⬇️")
	if err != nil {
		logger.Errorf("Failed to add reaction: %v", err)
		return err
	}

	if !videoUnavailable && !isDuplicate {
		player.ResumeOrStart(s, i.GuildID)
	}

	return nil
}

func editPromptEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, msg *discordgo.Message, embed *discordgo.MessageEmbed) {
	if discord.IsMessageCommand(i) {
		_, _ = s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, embed)
		return
	}

	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func confirmPlaylist(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistInfo *youtube.PlaylistInfo, voiceChannelID string) func() {
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

		discord.ClearPromptReactions(s, msg.ChannelID, msg.ID, "✅")

		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceChannelID, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)

	return func() {
		defer removeHandler()

		select {
		case <-confirmedChan:
			logger.Debugf("Reaction confirmed, handler complete")
		case <-time.After(playlistPromptExpiry):
			logger.Debugf("Timeout reached, cancelling")
			embed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutTitle, messages.T(originalInteraction.GuildID).Music.PlaylistTimeoutDesc)
			editPromptEmbed(s, originalInteraction, msg, embed)
			discord.ClearPromptReactions(s, msg.ChannelID, msg.ID, "✅")
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

func fetchPlaylistRest(i *discordgo.InteractionCreate, playlistID, videoID string) (*youtube.PlaylistInfo, error) {
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

func confirmPlaylistRest(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistID, videoID string, voiceChannelID string, timeoutEmbed *discordgo.MessageEmbed) func() {
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

		playlistInfo, err := fetchPlaylistRest(originalInteraction, playlistID, videoID)
		if err != nil {
			errorEmbed := messages.CreateErrorEmbed(messages.T(originalInteraction.GuildID).Titles.Error, messages.T(originalInteraction.GuildID).Music.PlaylistInfoFailed)
			editPromptEmbed(s, originalInteraction, msg, errorEmbed)
			discord.ClearPromptReactions(s, msg.ChannelID, msg.ID, "⬇️")
			return
		}

		loadingEmbed := messages.CreateWarningEmbed(messages.T(originalInteraction.GuildID).Music.PlaylistAddingTitle, messages.T(originalInteraction.GuildID).Music.PlaylistAddingRest)

		editPromptEmbed(s, originalInteraction, msg, loadingEmbed)

		discord.ClearPromptReactions(s, msg.ChannelID, msg.ID, "⬇️")

		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceChannelID, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)

	return func() {
		defer removeHandler()

		select {
		case <-confirmedChan:
			logger.Debugf("Reaction confirmed, handler complete")
		case <-time.After(playlistPromptExpiry):
			logger.Debugf("Timeout reached, cancelling")
			editPromptEmbed(s, originalInteraction, msg, timeoutEmbed)
			discord.ClearPromptReactions(s, msg.ChannelID, msg.ID, "⬇️")
		}
	}
}

func addPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, playlistInfo *youtube.PlaylistInfo, voiceChannelID string, messageID string) {
	release := playlistLocks.Acquire(i.GuildID)
	defer release()

	startTime := time.Now()
	logger.Debugf("Starting playlist processing for %d songs", len(playlistInfo.Videos))

	q, _ := queue.GetQueue(i.GuildID, false)
	isQueueEmpty := q == nil || len(q.Songs) == 0

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceChannelID); err != nil {
			logger.Errorf("Failed to create queue: %v", err)
			return
		}
	} else {
		if err := queue.UpdateVoiceChannel(i.GuildID, voiceChannelID); err != nil {
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
			player.ResumeOrStart(s, i.GuildID)
		}

		if len(playlistInfo.Videos) > 1 && addedCount > 0 {
			remainingSongs := playlistInfo.Videos[1:]
			logger.Debugf("Processing remaining %d songs (synchronously to maintain order)", len(remainingSongs))
			processPlaylistRest(s, i, remainingSongs, playlistInfo, startTime, messageID)
		}

		return
	}

	processPlaylist(s, i, playlistInfo.Videos, playlistInfo, startTime, messageID)
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
			markLive(song)
		}

		if !addPlaylistSong(guildID, song, "Error adding first song: %v") {
			skippedCount++
			continue
		}

		addedCount = 1
		logger.Debugf("First song added: %s", song.Title)
		break
	}

	return addedCount, skippedCount
}

func processPlaylistRest(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Debugf("Processing %d remaining songs with worker pool", len(songs))

	results := checkAvailability(songs)
	outcome := enqueueAvailable(i.GuildID, songs, results, true)

	totalTime := time.Since(startTime)
	logger.Debugf("Completed: %d added, %d skipped in %dms total",
		outcome.added, outcome.skipped, totalTime.Milliseconds())

	if shutdown.IsShuttingDown() {
		logger.Debug("Skipping completion message - bot is shutting down")
		return
	}

	description := fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistCompleteDesc,
		messages.FormatBoldMaskedLink(playlistInfo.Title, playlistInfo.URL))

	if outcome.skipped > 0 {
		description += fmt.Sprintf("\n\n"+messages.T(i.GuildID).Music.PlaylistSkippedCount, outcome.skipped)
	}

	successEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T(i.GuildID).Titles.PlaylistAdded,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Fields.TotalSongs, Value: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistSongsUnit, playlistInfo.VideoCount), Inline: true},
			{Name: messages.T(i.GuildID).Music.PlaylistAddedCount, Value: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistSongsUnit, outcome.added+1), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: i.Member.User.Username, Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
	}

	editCompletion(s, i, messageID, successEmbed)
	sendSkipNotice(s, i.GuildID, i.ChannelID, outcome.skippedSongs)
}

func processPlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Debugf("Standard processing for %d songs", len(songs))

	results := checkAvailability(songs)
	checkTime := time.Since(startTime)
	logger.Debugf("Availability check completed in %dms", checkTime.Milliseconds())

	outcome := enqueueAvailable(i.GuildID, songs, results, false)

	totalTime := time.Since(startTime)
	logger.Debugf("Completed: %d added, %d skipped in %dms", outcome.added, outcome.skipped, totalTime.Milliseconds())

	if shutdown.IsShuttingDown() {
		logger.Debug("Skipping completion message - bot is shutting down")
		return
	}

	description := messages.FormatBoldMaskedLink(playlistInfo.Title, playlistInfo.URL)
	if outcome.skipped > 0 {
		description += fmt.Sprintf("\n\n"+messages.T(i.GuildID).Music.PlaylistSkippedOrDup, outcome.skipped)
	}

	successEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T(i.GuildID).Titles.PlaylistAdded,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T(i.GuildID).Music.PlaylistAddedSongs, Value: fmt.Sprintf(messages.T(i.GuildID).Music.PlaylistSongsUnit, outcome.added), Inline: true},
			{Name: messages.T(i.GuildID).Fields.Requester, Value: i.Member.User.Username, Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
	}

	editCompletion(s, i, messageID, successEmbed)
	sendSkipNotice(s, i.GuildID, i.ChannelID, outcome.skippedSongs)

	q, _ := queue.GetQueue(i.GuildID, true)
	if q == nil || len(q.Songs) == 0 {
		return
	}
	player.ResumeOrStart(s, i.GuildID)
}

type playlistOutcome struct {
	added        int
	skipped      int
	skippedSongs []skippedSong
}

func markLive(song *youtube.Song) {
	song.IsLive = true
	song.Duration = youtube.LiveDuration
}

func addPlaylistSong(guildID string, song *youtube.Song, failureFormat string) bool {
	if err := queue.AddSong(guildID, queueSongFrom(song), -1); err != nil {
		if errors.Is(err, queue.ErrSongInQueue) {
			logger.Debugf("Skipping duplicate: %s", song.Title)
		} else {
			logger.Errorf(failureFormat, err)
		}
		return false
	}
	return true
}

func checkAvailability(songs []*youtube.Song) []youtube.BatchResult {
	jobs := make([]youtube.BatchJob, 0, len(songs))
	for i, song := range songs {
		jobs = append(jobs, youtube.BatchJob{
			URL:   song.URL,
			Index: i,
		})
	}
	return youtube.GetAvailabilityPool().CheckBatch(jobs)
}

func enqueueAvailable(guildID string, songs []*youtube.Song, results []youtube.BatchResult, isProgressLogged bool) playlistOutcome {
	var outcome playlistOutcome
	for i := range results {
		result := &results[i]
		song := songs[result.Index]

		if !result.Available && ytdlpUpdater.IsDefinitiveUnavailable(result.Error) {
			logger.Debugf("Skipping definitively unavailable: %s - %s",
				song.Title, result.Error)
			outcome.skipped++
			outcome.skippedSongs = append(outcome.skippedSongs, skippedSong{
				Title: song.Title, URL: song.URL, Thumbnail: song.Thumbnail, Error: result.Error,
			})
			continue
		}

		if result.IsLive {
			markLive(song)
		}

		if !addPlaylistSong(guildID, song, "Error adding song: %v") {
			outcome.skipped++
			continue
		}

		outcome.added++
		if isProgressLogged {
			logger.Debugf("Added song %d/%d: %s", outcome.added, len(songs), song.Title)
		}
	}
	return outcome
}

func editCompletion(s *discordgo.Session, i *discordgo.InteractionCreate, messageID string, embed *discordgo.MessageEmbed) {
	if _, err := s.ChannelMessageEditEmbed(i.ChannelID, messageID, embed); err != nil {
		logger.Errorf("Failed to update completion message: %v", err)
	}
}
