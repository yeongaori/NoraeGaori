package commands

import (
	"fmt"
	"math"
	"strconv"
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
	"noraegaori/pkg/logger"
)

// isDefinitiveUnavailableError checks if an error indicates the video is truly unavailable
// (geo-restricted, age-restricted, private, deleted) vs. a potential false negative
func isDefinitiveUnavailableError(errorMsg string) bool {
	errorLower := strings.ToLower(errorMsg)
	// These patterns indicate the video is truly unavailable, not a false negative
	definitivePatterns := []string{
		"video unavailable",
		"not available",
		"private video",
		"deleted video",
		"age-restricted",
		"age restricted",
		"not available in your country",
		"geo",
		"members-only",
		"members only",
		"premium",
		"copyright",
		"blocked",
		"removed by the uploader",
		"account associated with this video has been terminated",
	}
	for _, pattern := range definitivePatterns {
		if strings.Contains(errorLower, pattern) {
			return true
		}
	}
	return false
}

// cleanErrorMessage extracts the main error reason from verbose yt-dlp error messages
func cleanErrorMessage(errorMsg string) string {
	errorLower := strings.ToLower(errorMsg)
	t := messages.T()
	errorMappings := map[string]string{
		"private video":              t.Music.ErrorPrivateVideo,
		"deleted video":              t.Music.ErrorDeletedVideo,
		"age-restricted":             t.Music.ErrorAgeRestricted,
		"age restricted":             t.Music.ErrorAgeRestricted,
		"not available in your country": t.Music.ErrorGeoRestricted,
		"geo":                        t.Music.ErrorGeoRestricted,
		"members-only":               t.Music.ErrorMembersOnly,
		"members only":               t.Music.ErrorMembersOnly,
		"premium":                    t.Music.ErrorPremiumOnly,
		"copyright":                  t.Music.ErrorCopyright,
		"blocked":                    t.Music.ErrorBlocked,
	}
	for pattern, message := range errorMappings {
		if strings.Contains(errorLower, pattern) {
			return message
		}
	}
	return t.Music.ErrorUnavailable
}

// voteSession tracks an active vote with expiration support
type voteSession struct {
	votes          map[string]bool // userID -> voted
	requiredVotes  int
	startTime      time.Time
	cancelTimer    chan bool
	messageID      string
	channelID      string
	voiceChannelID string
}

// Skip vote tracking
var (
	skipVotes      = make(map[string]*voteSession) // guildID -> vote session
	skipVotesMutex sync.RWMutex
)

// Stop vote tracking
var (
	stopVotes      = make(map[string]*voteSession) // guildID -> vote session
	stopVotesMutex sync.RWMutex
)

const voteExpirationTime = 60 * time.Second // Votes expire after 60 seconds

// startVoteWithReaction starts a vote timer with reaction-based voting support
func startVoteWithReaction(s *discordgo.Session, guildID, title, emoji string, vs *voteSession, votesMap map[string]*voteSession, votesMutex *sync.RWMutex, onVotePassed func(currentVotes int)) {
	// Add reaction emoji to the vote message
	if err := s.MessageReactionAdd(vs.channelID, vs.messageID, emoji); err != nil {
		logger.Errorf("[VoteReaction] Failed to add reaction to message: %v", err)
	}

	voteDone := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if r.UserID == s.State.User.ID {
			return
		}
		if r.MessageID != vs.messageID {
			return
		}
		if r.Emoji.Name != emoji {
			return
		}

		member, err := s.State.Member(guildID, r.UserID)
		if err != nil || member.User.Bot {
			return
		}

		voiceState, err := s.State.VoiceState(guildID, r.UserID)
		if err != nil || voiceState.ChannelID != vs.voiceChannelID {
			return
		}

		votesMutex.Lock()
		if votesMap[guildID] != vs {
			votesMutex.Unlock()
			return
		}
		if vs.votes[r.UserID] {
			votesMutex.Unlock()
			return
		}
		vs.votes[r.UserID] = true
		currentVotes := len(vs.votes)
		requiredVotes := vs.requiredVotes

		if currentVotes >= requiredVotes {
			delete(votesMap, guildID)
			votesMutex.Unlock()

			onVotePassed(currentVotes)

			select {
			case voteDone <- true:
			default:
			}
		} else {
			votesMutex.Unlock()

			remaining := int(voteExpirationTime.Seconds()) - int(time.Since(vs.startTime).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			embed := messages.CreateWarningEmbed(title, "")
			messages.AddField(embed, messages.FieldCurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			messages.SetFooter(embed, fmt.Sprintf(messages.FooterVoteReaction, emoji, remaining))
			s.ChannelMessageEditEmbed(vs.channelID, vs.messageID, embed)
		}
	}

	removeHandler := s.AddHandler(reactionHandler)
	defer removeHandler()

	select {
	case <-vs.cancelTimer:
		logger.Debugf("[VoteReaction] %s vote cancelled for guild %s", title, guildID)
		s.MessageReactionsRemoveAll(vs.channelID, vs.messageID)
	case <-voteDone:
		logger.Debugf("[VoteReaction] %s vote passed via reaction for guild %s", title, guildID)
		s.MessageReactionsRemoveAll(vs.channelID, vs.messageID)
	case <-time.After(voteExpirationTime):
		logger.Infof("[VoteReaction] %s vote expired for guild %s", title, guildID)
		votesMutex.Lock()
		delete(votesMap, guildID)
		votesMutex.Unlock()

		embed := messages.CreateWarningEmbed(title, messages.T().Votes.Expired)
		s.ChannelMessageEditEmbed(vs.channelID, vs.messageID, embed)
		s.MessageReactionsRemoveAll(vs.channelID, vs.messageID)
	}
}

// Playlist processing locks per guild to prevent order mixing
var (
	playlistLocks   = make(map[string]*sync.Mutex)
	playlistLocksMu sync.Mutex
)

// getPlaylistLock gets or creates a playlist processing lock for a guild
func getPlaylistLock(guildID string) *sync.Mutex {
	playlistLocksMu.Lock()
	defer playlistLocksMu.Unlock()

	if _, exists := playlistLocks[guildID]; !exists {
		playlistLocks[guildID] = &sync.Mutex{}
	}
	return playlistLocks[guildID]
}

// stripMarkdown removes common markdown formatting from a string
// This handles cases where URLs are pasted with markdown formatting (e.g., **URL**)
func stripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	// Remove bold/italic (**text** or __text__)
	if strings.HasPrefix(s, "**") && strings.HasSuffix(s, "**") && len(s) > 4 {
		s = s[2 : len(s)-2]
	} else if strings.HasPrefix(s, "__") && strings.HasSuffix(s, "__") && len(s) > 4 {
		s = s[2 : len(s)-2]
	} else if strings.HasPrefix(s, "*") && strings.HasSuffix(s, "*") && len(s) > 2 {
		s = s[1 : len(s)-1]
	} else if strings.HasPrefix(s, "_") && strings.HasSuffix(s, "_") && len(s) > 2 {
		s = s[1 : len(s)-1]
	}
	// Remove angle brackets (common in Discord URL formatting)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) > 2 {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// HandlePlayNext handles the playnext command (adds song at position 2)
func HandlePlayNext(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Get query from options (before defer — no need to defer for simple validation errors)
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.EnterQuery))
		return nil
	}
	query := options[0].StringValue()

	// Strip markdown formatting from query
	query = stripMarkdown(query)

	// Check if user is in a voice channel (before defer — fast fail)
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorNotInVoiceChannel))
		return nil
	}

	// Defer response for long-running operation
	DeferResponse(s, i)

	// Show searching message
	searchEmbed := messages.CreateWarningEmbed(messages.TitleSearching, fmt.Sprintf(messages.DescSearching, query))
	UpdateResponseEmbed(s, i, searchEmbed)

	// Search for the song
	logger.Infof("[PlayNext] Searching for: %s", query)
	song, err := youtube.Search(query, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorSongNotFound))
		return err
	}

	// Create queue if doesn't exist
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.QueueCreateFailed))
			return err
		}
	} else {
		// Update voice channel to user's current channel (handles bot restart / channel switch)
		queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID)
	}

	// Add song to queue at position 1 (next song)
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

	if err := queue.AddSong(i.GuildID, queueSong, 1); err != nil {
		// Check if it's a duplicate error
		if err.Error() == "song already in queue: "+song.Title {
			UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(messages.TitleDuplicate, messages.ErrorDuplicateSong))
		} else {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Music.SongAddFailed, err)))
		}
		return err
	}

	// Get updated queue
	q, _ = queue.GetQueue(i.GuildID, true)

	// Show success message with "added as next"
	embed := messages.CreateSongEmbed(
		messages.ColorSuccess,
		messages.T().Music.AddedAsNext,
		"",
		song.Title, song.URL, song.Uploader,
		song.Duration, i.Member.User.Username,
		song.Thumbnail,
	)

	// If this is the first song, start playing
	if len(q.Songs) == 1 {
		go player.Play(s, i.GuildID)
	}

	UpdateResponseEmbed(s, i, embed)
	return nil
}

// HandlePlay handles the play command
func HandlePlay(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Get query from options (before defer — no need to defer for simple validation errors)
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.EnterQuery))
		return nil
	}
	query := options[0].StringValue()

	// Strip markdown formatting from query (e.g., **URL** -> URL)
	query = stripMarkdown(query)

	// Check if user is in a voice channel (before defer — fast fail)
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorNotInVoiceChannel))
		return nil
	}

	// Defer response for long-running operation
	DeferResponse(s, i)

	// Show searching message
	searchEmbed := messages.CreateWarningEmbed(messages.TitleSearching, fmt.Sprintf(messages.DescSearching, query))
	UpdateResponseEmbed(s, i, searchEmbed)

	// Check if it's a YouTube URL and analyze it
	if youtube.IsYouTubeURL(query) {
		analysis := youtube.AnalyzeYouTubeURL(query)
		logger.Debugf("[Play] URL analysis: type=%s, videoID=%s, playlistID=%s", analysis.Type, analysis.VideoID, analysis.PlaylistID)

		// Handle pure playlist
		if analysis.Type == youtube.URLTypePurePlaylist {
			return handlePurePlaylist(s, i, query, voiceState)
		}

		// Handle video with playlist
		if analysis.Type == youtube.URLTypeVideoWithPlaylist {
			return handleVideoWithPlaylist(s, i, query, analysis, voiceState)
		}
	}

	// Regular search or single video handling
	logger.Infof("[Play] Searching for: %s", query)
	song, err := youtube.Search(query, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorSongNotFound))
		return err
	}

	// Create queue if doesn't exist
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.QueueCreateFailed))
			return err
		}
	} else {
		// Update voice channel to user's current channel (handles bot restart / channel switch)
		queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID)
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

	if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
		// Check if it's a duplicate error
		if err.Error() == "song already in queue: "+song.Title {
			UpdateResponseEmbed(s, i, messages.CreateWarningEmbed(messages.TitleDuplicate, messages.ErrorDuplicateSong))
		} else {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Music.SongAddFailed, err)))
		}
		return err
	}

	// Get updated queue
	q, _ = queue.GetQueue(i.GuildID, true)

	// Check if this is the first song (will start playing immediately)
	isFirstSong := len(q.Songs) == 1

	var embed *discordgo.MessageEmbed
	if isFirstSong {
		// Show loading message for first song with more details
		embed = &discordgo.MessageEmbed{
			Color:       messages.ColorWarning,
			Title:       messages.TitleLoading,
			Description: fmt.Sprintf("**[%s](%s)**\n\n%s", messages.EscapeMarkdown(song.Title), song.URL, messages.DescLoading),
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
			Fields: []*discordgo.MessageEmbedField{
				{Name: messages.FieldUploader, Value: song.Uploader, Inline: true},
				{Name: messages.FieldDuration, Value: song.Duration, Inline: true},
				{Name: messages.FieldRequester, Value: i.Member.User.Username, Inline: true},
			},
		}
	} else {
		// Show added to queue message
		embed = messages.CreateSongEmbed(
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
	}

	UpdateResponseEmbed(s, i, embed)

	// Store loading message for later update (stored globally by guild ID)
	if isFirstSong {
		msg, err := GetResponseMessage(s, i)
		if err == nil {
			player.SetLoadingMessage(i.GuildID, msg)
		}
	}

	// Start playing if not already playing
	p := player.GetPlayer(i.GuildID)
	if !p.Playing && !p.Loading {
		go player.Play(s, i.GuildID)
	}

	return nil
}

// handlePurePlaylist handles a pure playlist URL
func handlePurePlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, playlistURL string, voiceState *discordgo.VoiceState) error {
	// Fetch playlist info
	playlistInfo, err := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.PlaylistInfoFailed))
		return err
	}

	// Show playlist confirmation
	confirmEmbed := &discordgo.MessageEmbed{
		Color: messages.ColorInfo,
		Title: messages.TitlePlaylistFound,
		Description: fmt.Sprintf(messages.T().Music.PlaylistConfirmDesc,
			messages.EscapeMarkdown(playlistInfo.Title), playlistInfo.URL, playlistInfo.VideoCount),
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
		Footer:    &discordgo.MessageEmbedFooter{Text: messages.T().Music.PlaylistConfirmFooter},
	}

	// Send confirmation message without components
	UpdateResponseEmbed(s, i, confirmEmbed)

	// Get the response message to add reaction
	msg, err := GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("[Playlist] Failed to get interaction response: %v", err)
		return err
	}

	// Add reaction
	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "✅")
	if err != nil {
		logger.Errorf("[Playlist] Failed to add reaction: %v", err)
		return err
	}

	// Handle reaction interaction with timeout
	go handlePlaylistConfirmationReaction(s, i, msg, playlistInfo, voiceState)

	return nil
}

// handleVideoWithPlaylist handles a video URL that's part of a playlist
func handleVideoWithPlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, videoURL string, analysis *youtube.URLAnalysis, voiceState *discordgo.VoiceState) error {
	// First, try to add the specific video
	// Use a clean video URL without playlist parameter to avoid yt-dlp issues
	cleanVideoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", analysis.VideoID)
	song, videoErr := youtube.GetVideoInfo(cleanVideoURL, i.Member.User.Username, i.Member.User.ID)
	videoUnavailable := videoErr != nil

	// If direct video fetch failed, try to get video info from the playlist
	// Some videos are accessible via playlist but not directly
	if videoUnavailable {
		logger.Debugf("[Play] Direct video fetch failed, trying to get info from playlist")
		playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", analysis.PlaylistID)
		playlistInfo, playlistErr := youtube.GetPlaylistInfo(playlistURL, i.Member.User.Username, i.Member.User.ID)
		if playlistErr == nil {
			// First, try to find the video by ID in the playlist
			for _, video := range playlistInfo.Videos {
				if strings.Contains(video.URL, analysis.VideoID) {
					logger.Infof("[Play] Found video in playlist by ID, using playlist info: %s", video.Title)
					song = video
					videoUnavailable = false
					break
				}
			}
			// If not found by ID, use the first video from the playlist as fallback
			// This handles cases where the video was re-uploaded with a different ID
			if videoUnavailable && len(playlistInfo.Videos) > 0 {
				song = playlistInfo.Videos[0]
				logger.Infof("[Play] Video ID not in playlist, using first video: %s", song.Title)
				videoUnavailable = false
			}
		}
	}

	// Create queue if doesn't exist
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil {
		return err
	}

	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.QueueCreateFailed))
			return err
		}
	}

	isDuplicate := false
	var embed *discordgo.MessageEmbed

	if videoUnavailable {
		// Video is unavailable even from playlist, offer to add the rest of the playlist
		logger.Warnf("[Play] Specific video unavailable (even from playlist), offering playlist: %v", videoErr)

		embed = &discordgo.MessageEmbed{
			Color:       messages.ColorWarning,
			Title:       messages.T().Music.VideoUnavailableTitle,
			Description: messages.T().Music.VideoUnavailableDesc,
			Footer:      &discordgo.MessageEmbedFooter{Text: messages.T().Music.VideoUnavailableFooter},
		}
	} else {
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

		if err := queue.AddSong(i.GuildID, queueSong, -1); err != nil {
			if err.Error() == "song already in queue: "+song.Title {
				isDuplicate = true
			} else {
				UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Music.SongAddFailed, err)))
				return err
			}
		}

		// Show message about the video and ask about playlist
		var description string
		if isDuplicate {
			description = fmt.Sprintf(messages.T().Music.VideoWithPlaylistDuplicate,
				messages.EscapeMarkdown(song.Title), song.URL)
		} else {
			description = fmt.Sprintf(messages.T().Music.VideoWithPlaylistFound,
				messages.EscapeMarkdown(song.Title), song.URL)
		}

		embed = &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       messages.TitleAdded,
			Description: description,
			Fields: []*discordgo.MessageEmbedField{
				{Name: messages.FieldUploader, Value: messages.EscapeMarkdown(song.Uploader), Inline: true},
				{Name: messages.FieldDuration, Value: song.Duration, Inline: true},
				{Name: messages.FieldRequester, Value: messages.EscapeMarkdown(song.RequestedBy), Inline: true},
			},
			Thumbnail: &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
			Footer:    &discordgo.MessageEmbedFooter{Text: messages.T().Music.VideoWithPlaylistFooter},
		}
	}

	UpdateResponseEmbed(s, i, embed)

	// Get the response message to add reaction
	msg, err := GetResponseMessage(s, i)
	if err != nil {
		logger.Errorf("[Play] Failed to get interaction response: %v", err)
		return err
	}

	// Add reaction
	err = s.MessageReactionAdd(msg.ChannelID, msg.ID, "⬇️")
	if err != nil {
		logger.Errorf("[Play] Failed to add reaction: %v", err)
		return err
	}

	// Start playing if this was the first song and not a duplicate and video was available
	if !videoUnavailable && !isDuplicate {
		q, _ = queue.GetQueue(i.GuildID, true)
		p := player.GetPlayer(i.GuildID)
		if len(q.Songs) == 1 && !p.Playing && !p.Loading {
			go player.Play(s, i.GuildID)
		}
	}

	// Handle reaction interaction for adding rest of playlist
	// When video is unavailable, don't exclude any video ID (pass empty string)
	excludeVideoID := analysis.VideoID
	if videoUnavailable {
		excludeVideoID = "" // Don't exclude the unavailable video, let playlist processing skip it naturally
	}
	go handlePlaylistRestConfirmationReaction(s, i, msg, analysis.PlaylistID, excludeVideoID, voiceState)

	return nil
}

// HandlePause handles the pause command
func HandlePause(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Check if user is in voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorNotInVoiceChannel))
		return nil
	}

	// Force refresh queue to get accurate playing/loading state
	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || (!q.Playing && !q.Loading) {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.NotPlayingOrLoading))
		return nil
	}

	// Pause the player
	if err := player.Pause(i.GuildID); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.PauseFailed))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.TitlePaused, messages.DescPaused))
	return nil
}

// HandleResume handles the resume command
func HandleResume(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Check if user is in voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.ErrorNotInVoiceChannel))
		return nil
	}

	// Force refresh from database to get latest paused state
	q, err := queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.NoSongsToResume))
		return nil
	}

	// Check if already playing
	if q.Playing || q.Loading {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.AlreadyPlaying))
		return nil
	}

	// Clear paused state in database
	if err := queue.SetPaused(i.GuildID, false); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.PlaybackStartError))
		return err
	}

	// Update voice channel to user's current channel before resuming
	if err := queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.PlaybackStartError))
		return err
	}

	// Get fresh queue after updates
	q, err = queue.GetQueue(i.GuildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.QueueNotFound))
		return nil
	}

	currentSong := q.Songs[0]

	// Check if current song is a live stream that has ended
	if currentSong.IsLive {
		logger.Debugf("[Resume] Current song is a live stream, checking if it's still live")

		// Defer response first since live stream check can take time
		DeferResponse(s, i)

		// Show loading message while checking
		checkingEmbed := &discordgo.MessageEmbed{
			Color:       messages.ColorWarning,
			Title:       messages.T().Music.LiveCheckingTitle,
			Description: fmt.Sprintf(messages.T().Music.LiveCheckingDesc,
				messages.EscapeMarkdown(currentSong.Title), currentSong.URL),
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: currentSong.Thumbnail},
		}
		UpdateResponseEmbed(s, i, checkingEmbed)

		// Check live stream status using youtube module
		isStillLive, err := youtube.CheckIfLive(currentSong.URL)
		if err != nil {
			logger.Warnf("[Resume] Error checking live stream status: %v", err)
			// If we can't check, proceed anyway
		} else if !isStillLive {
			logger.Infof("[Resume] Live stream has ended, skipping to next song")

			// Remove the ended live stream from queue
			if err := queue.RemoveSong(i.GuildID, 0); err != nil {
				UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.PlaybackStartError))
				return err
			}

			// Get updated queue after removal
			q, err = queue.GetQueue(i.GuildID, true)
			if err != nil || q == nil || len(q.Songs) == 0 {
				embed := messages.CreateWarningEmbed(messages.T().Music.LiveEndedTitle,
					messages.T().Music.LiveEndedNoQueue)
				UpdateResponseEmbed(s, i, embed)
				return nil
			}

			// Show message about skipping
			skipEmbed := &discordgo.MessageEmbed{
				Color:       messages.ColorWarning,
				Title:       messages.T().Music.LiveEndedTitle,
				Description: fmt.Sprintf(messages.T().Music.LiveEndedSkip,
					messages.EscapeMarkdown(currentSong.Title), currentSong.URL),
			}
			UpdateResponseEmbed(s, i, skipEmbed)

			// Start playing the next song
			go player.Play(s, i.GuildID)
			return nil
		}

		logger.Infof("[Resume] Live stream is still live, proceeding with resume")

		// Update message and start playback
		successEmbed := messages.CreateSuccessEmbed(messages.T().Music.LiveStartTitle, messages.T().Music.LiveStartDesc)
		UpdateResponseEmbed(s, i, successEmbed)

		go player.Play(s, i.GuildID)
		return nil
	}

	// Show loading message if seeking to a position far into the song (>2 minutes)
	const seekLoadingThreshold = 120000 // 2 minutes in milliseconds
	if currentSong.SeekTime > seekLoadingThreshold {
		DeferResponse(s, i)

		loadingEmbed := &discordgo.MessageEmbed{
			Color:       messages.ColorWarning,
			Title:       messages.TitleLoading,
			Description: fmt.Sprintf("**[%s](%s)**\n\n%s",
				messages.EscapeMarkdown(currentSong.Title), currentSong.URL, messages.DescLoading),
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: currentSong.Thumbnail},
		}
		UpdateResponseEmbed(s, i, loadingEmbed)

		// Store loading message for later update (stored globally by guild ID)
		msg, err := GetResponseMessage(s, i)
		if err == nil {
			player.SetLoadingMessage(i.GuildID, msg)
		}

		// Start playing from the saved position
		go player.Play(s, i.GuildID)
		return nil
	}

	// Start playing from the saved position
	go player.Play(s, i.GuildID)

	// Show success message
	successEmbed := messages.CreateSuccessEmbed(messages.T().Music.ResumeStartTitle, messages.T().Music.ResumeStartDesc)
	RespondEmbed(s, i, successEmbed)
	return nil
}

// HandleSkip handles the skip command with voting system
func HandleSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	// Check if user is in voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.EnterVoiceChannel))
		return nil
	}

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoSong, messages.ErrorEmptyQueue))
		return nil
	}

	// Get guild to access voice states
	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.ServerInfoFailed))
		return err
	}

	// Count non-bot members in the same voice channel
	voiceMembers := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == voiceState.ChannelID {
			member, err := s.State.Member(i.GuildID, vs.UserID)
			if err == nil && !member.User.Bot {
				voiceMembers++
			}
		}
	}

	requiredVotes := int(math.Ceil(float64(voiceMembers) * 0.5))

	// If only 1-2 members (requiredVotes = 1), skip immediately
	if requiredVotes == 1 {
		songTitle := q.Songs[0].Title
		songThumbnail := q.Songs[0].Thumbnail

		// Wait for skip to complete and show result
		err := player.Skip(s, i.GuildID)
		if err != nil && err != player.ErrQueueEmpty {
			logger.Errorf("[Skip] Failed to skip: %v", err)
			embed := messages.CreateErrorEmbed(messages.T().Music.SkipFailedTitle,
				fmt.Sprintf(messages.T().Music.SkipFailedDesc, err))
			UpdateResponseEmbed(s, i, embed)
			return nil // Error already displayed, don't trigger another error message
		}

		// Check if queue became empty
		if err == player.ErrQueueEmpty {
			// Send skip success + queue ended message
			embed := messages.CreateSuccessEmbed(messages.T().Music.PlaybackEndedTitle,
				fmt.Sprintf(messages.T().Music.PlaybackEndedSkip, messages.EscapeMarkdown(songTitle)))
			messages.SetThumbnail(embed, songThumbnail)
			UpdateResponseEmbed(s, i, embed)

			// Cleanup: leave voice, delete queue
			if stopErr := player.Stop(i.GuildID); stopErr != nil {
				logger.Errorf("[Skip] Failed to cleanup after queue empty: %v", stopErr)
			}
			return nil
		}

		// Update the deferred response message (instead of sending new message)
		embed := messages.CreateSuccessEmbed(messages.TitleSkipped,
			fmt.Sprintf(messages.DescSkipped, messages.EscapeMarkdown(songTitle)))
		messages.SetThumbnail(embed, songThumbnail)
		UpdateResponseEmbed(s, i, embed)

		return nil
	}

	// Voting logic
	songTitle := q.Songs[0].Title
	songThumbnail := q.Songs[0].Thumbnail

	isNewSession := false
	skipVotesMutex.Lock()

	// Initialize vote session if needed
	session := skipVotes[i.GuildID]
	if session == nil {
		session = &voteSession{
			votes:          make(map[string]bool),
			requiredVotes:  requiredVotes,
			startTime:      time.Now(),
			cancelTimer:    make(chan bool, 1),
			voiceChannelID: voiceState.ChannelID,
		}
		skipVotes[i.GuildID] = session
		isNewSession = true
	}
	skipVotesMutex.Unlock()

	// Check if already voted
	skipVotesMutex.Lock()
	if session.votes[i.Member.User.ID] {
		skipVotesMutex.Unlock()
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleAlreadyVoted, messages.ErrorAlreadyVoted))
		return nil
	}

	// Add vote
	session.votes[i.Member.User.ID] = true
	currentVotes := len(session.votes)
	skipVotesMutex.Unlock()

	if currentVotes >= requiredVotes {
		// Vote succeeded - cancel timer and clear votes
		select {
		case session.cancelTimer <- true:
		default:
		}

		skipVotesMutex.Lock()
		delete(skipVotes, i.GuildID)
		skipVotesMutex.Unlock()

		// Wait for skip to complete and show result
		err := player.Skip(s, i.GuildID)
		if err != nil && err != player.ErrQueueEmpty {
			logger.Errorf("[Skip] Failed to skip: %v", err)
			embed := messages.CreateErrorEmbed(messages.T().Music.SkipFailedTitle,
				fmt.Sprintf(messages.T().Music.SkipFailedDesc, err))
			messages.AddField(embed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			UpdateResponseEmbed(s, i, embed)
			return nil
		}

		if err == player.ErrQueueEmpty {
			embed := messages.CreateSuccessEmbed(messages.T().Music.PlaybackEndedTitle,
				fmt.Sprintf(messages.T().Music.PlaybackEndedSkip, messages.EscapeMarkdown(songTitle)))
			messages.SetThumbnail(embed, songThumbnail)
			messages.AddField(embed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			UpdateResponseEmbed(s, i, embed)

			if stopErr := player.Stop(i.GuildID); stopErr != nil {
				logger.Errorf("[Skip] Failed to cleanup after queue empty: %v", stopErr)
			}
			return nil
		}

		embed := messages.CreateSuccessEmbed(messages.TitleSkipped,
			fmt.Sprintf(messages.DescSkipped, messages.EscapeMarkdown(songTitle)))
		messages.SetThumbnail(embed, songThumbnail)
		messages.AddField(embed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		UpdateResponseEmbed(s, i, embed)
	} else {
		// Not enough votes yet
		embed := messages.CreateWarningEmbed(messages.TitleSkipVote, "")
		messages.AddField(embed, messages.FieldCurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		messages.SetFooter(embed, fmt.Sprintf(messages.FooterVoteReaction, "⏭", int(voteExpirationTime.Seconds())))
		UpdateResponseEmbed(s, i, embed)

		// Start reaction voting for new sessions
		if isNewSession {
			msg, msgErr := GetResponseMessage(s, i)
			if msgErr == nil && msg != nil {
				session.messageID = msg.ID
				session.channelID = msg.ChannelID

				go startVoteWithReaction(s, i.GuildID, messages.TitleSkipVote, "⏭", session, skipVotes, &skipVotesMutex, func(votes int) {
					skipErr := player.Skip(s, i.GuildID)
					if skipErr != nil && skipErr != player.ErrQueueEmpty {
						errEmbed := messages.CreateErrorEmbed(messages.T().Music.SkipFailedTitle, fmt.Sprintf(messages.T().Music.SkipFailedDesc, skipErr))
						s.ChannelMessageEditEmbed(session.channelID, session.messageID, errEmbed)
						return
					}
					if skipErr == player.ErrQueueEmpty {
						doneEmbed := messages.CreateSuccessEmbed(messages.T().Music.PlaybackEndedTitle,
							fmt.Sprintf(messages.T().Music.PlaybackEndedSkip, messages.EscapeMarkdown(songTitle)))
						messages.SetThumbnail(doneEmbed, songThumbnail)
						messages.AddField(doneEmbed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", votes, requiredVotes), true)
						s.ChannelMessageEditEmbed(session.channelID, session.messageID, doneEmbed)
						player.Stop(i.GuildID)
						return
					}
					skipEmbed := messages.CreateSuccessEmbed(messages.TitleSkipped,
						fmt.Sprintf(messages.DescSkipped, messages.EscapeMarkdown(songTitle)))
					messages.SetThumbnail(skipEmbed, songThumbnail)
					messages.AddField(skipEmbed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", votes, requiredVotes), true)
					s.ChannelMessageEditEmbed(session.channelID, session.messageID, skipEmbed)
				})
			}
		}
	}

	return nil
}

// ClearSkipVotes clears skip votes for a guild (called when song changes)
func ClearSkipVotes(guildID string) {
	skipVotesMutex.Lock()
	defer skipVotesMutex.Unlock()

	// Cancel timer before deleting
	if session := skipVotes[guildID]; session != nil {
		select {
		case session.cancelTimer <- true:
		default:
		}
	}

	delete(skipVotes, guildID)
}

// ClearStopVotes clears stop votes for a guild (called when song changes)
func ClearStopVotes(guildID string) {
	stopVotesMutex.Lock()
	defer stopVotesMutex.Unlock()

	if session := stopVotes[guildID]; session != nil {
		select {
		case session.cancelTimer <- true:
		default:
		}
	}

	delete(stopVotes, guildID)
}

// HandleStop handles the stop command with voting system
func HandleStop(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	// Check if user is in voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.EnterVoiceChannel))
		return nil
	}

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoSong, messages.ErrorEmptyQueue))
		return nil
	}

	// Get guild to access voice states
	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.ServerInfoFailed))
		return err
	}

	// Count non-bot members in the same voice channel
	voiceMembers := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == voiceState.ChannelID {
			member, err := s.State.Member(i.GuildID, vs.UserID)
			if err == nil && !member.User.Bot {
				voiceMembers++
			}
		}
	}

	requiredVotes := int(math.Ceil(float64(voiceMembers) * 0.5))
	if requiredVotes < 1 {
		requiredVotes = 1
	}

	// If only 1-2 members (requiredVotes = 1), stop immediately
	if requiredVotes == 1 {
		if err := player.Stop(i.GuildID); err != nil {
			UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T().Music.StopFailedTitle, fmt.Sprintf(messages.T().Music.StopFailedDesc, err)))
			return nil
		}
		UpdateResponseEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Music.StopSuccessTitle, messages.T().Music.StopSuccessDesc))
		return nil
	}

	// Voting logic
	isNewSession := false
	stopVotesMutex.Lock()

	// Initialize vote session if needed
	session := stopVotes[i.GuildID]
	if session == nil {
		session = &voteSession{
			votes:          make(map[string]bool),
			requiredVotes:  requiredVotes,
			startTime:      time.Now(),
			cancelTimer:    make(chan bool, 1),
			voiceChannelID: voiceState.ChannelID,
		}
		stopVotes[i.GuildID] = session
		isNewSession = true
	}
	stopVotesMutex.Unlock()

	// Check if already voted
	stopVotesMutex.Lock()
	if session.votes[i.Member.User.ID] {
		stopVotesMutex.Unlock()
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleAlreadyVoted, messages.T().Music.StopAlreadyVoted))
		return nil
	}

	// Add vote
	session.votes[i.Member.User.ID] = true
	currentVotes := len(session.votes)
	stopVotesMutex.Unlock()

	if currentVotes >= requiredVotes {
		// Vote succeeded - cancel timer and clear votes
		select {
		case session.cancelTimer <- true:
		default:
		}

		stopVotesMutex.Lock()
		delete(stopVotes, i.GuildID)
		stopVotesMutex.Unlock()

		// Stop playback
		if err := player.Stop(i.GuildID); err != nil {
			embed := messages.CreateErrorEmbed(messages.T().Music.StopFailedTitle, fmt.Sprintf(messages.T().Music.StopFailedDesc, err))
			messages.AddField(embed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
			UpdateResponseEmbed(s, i, embed)
			return nil
		}

		embed := messages.CreateSuccessEmbed(messages.T().Music.StopSuccessTitle, messages.T().Music.StopSuccessDesc)
		messages.AddField(embed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		UpdateResponseEmbed(s, i, embed)
	} else {
		// Not enough votes yet
		embed := messages.CreateWarningEmbed(messages.TitleStopVote, "")
		messages.AddField(embed, messages.FieldCurrentVote, fmt.Sprintf("%d/%d", currentVotes, requiredVotes), true)
		messages.SetFooter(embed, fmt.Sprintf(messages.FooterVoteReaction, "⏹", int(voteExpirationTime.Seconds())))
		UpdateResponseEmbed(s, i, embed)

		// Start reaction voting for new sessions
		if isNewSession {
			msg, msgErr := GetResponseMessage(s, i)
			if msgErr == nil && msg != nil {
				session.messageID = msg.ID
				session.channelID = msg.ChannelID

				go startVoteWithReaction(s, i.GuildID, messages.TitleStopVote, "⏹", session, stopVotes, &stopVotesMutex, func(votes int) {
					if stopErr := player.Stop(i.GuildID); stopErr != nil {
						errEmbed := messages.CreateErrorEmbed(messages.T().Music.StopFailedTitle, fmt.Sprintf(messages.T().Music.StopFailedDesc, stopErr))
						s.ChannelMessageEditEmbed(session.channelID, session.messageID, errEmbed)
						return
					}
					stopEmbed := messages.CreateSuccessEmbed(messages.T().Music.StopSuccessTitle, messages.T().Music.StopSuccessDesc)
					messages.AddField(stopEmbed, messages.FieldVoteResult, fmt.Sprintf("%d/%d", votes, requiredVotes), true)
					s.ChannelMessageEditEmbed(session.channelID, session.messageID, stopEmbed)
				})
			}
		}
	}

	return nil
}

// HandleNowPlaying handles the nowplaying command
func HandleNowPlaying(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoSong, messages.ErrorEmptyQueue))
		return nil
	}

	song := q.Songs[0]

	// Determine title and color based on status
	var title string
	var color int
	if q.Loading {
		title = messages.T().Music.NowPlayingLoading
		color = messages.ColorWarning
	} else if q.Playing {
		title = messages.T().Music.NowPlayingPlaying
		color = messages.ColorSuccess
	} else {
		title = messages.T().Music.NowPlayingPaused
		color = messages.ColorPaused
	}

	// Calculate current position if playing (and not a live stream)
	progressText := song.Duration
	if q.Playing && !song.IsLive {
		position := player.GetCurrentPosition(i.GuildID)
		positionStr := player.FormatDuration(position)
		progressText = fmt.Sprintf("%s / %s", positionStr, song.Duration)
	}

	embed := &discordgo.MessageEmbed{
		Color:       color,
		Title:       title,
		Description: fmt.Sprintf("**[%s](%s)**", messages.EscapeMarkdown(song.Title), song.URL),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.FieldUploader, Value: messages.EscapeMarkdown(song.Uploader), Inline: true},
			{Name: messages.FieldDuration, Value: progressText, Inline: true},
			{Name: messages.FieldRequester, Value: messages.EscapeMarkdown(song.RequestedByTag), Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: song.Thumbnail},
	}

	// Add next song if exists
	if len(q.Songs) > 1 {
		messages.AddField(embed, messages.FieldNextSong, fmt.Sprintf("**%s**", messages.EscapeMarkdown(q.Songs[1].Title)), false)
	}

	RespondEmbed(s, i, embed)
	return nil
}

// HandleVolume handles the volume command
func HandleVolume(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	// If no volume specified, show current volume
	if len(options) == 0 {
		volume, err := queue.GetVolume(i.GuildID)
		if err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Music.VolumeQueryFailed, err)))
			return err
		}
		RespondEmbed(s, i, messages.CreateInfoEmbed(messages.T().Music.CurrentVolumeTitle, fmt.Sprintf(messages.T().Music.CurrentVolumeDesc, volume)))
		return nil
	}

	// Handle both string (text commands) and float64 (slash commands)
	var volume float64
	switch v := options[0].Value.(type) {
	case float64:
		volume = v
	case string:
		var err error
		volume, err = strconv.ParseFloat(v, 64)
		if err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.VolumeNotNumber))
			return nil
		}
	default:
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.VolumeNotNumber))
		return nil
	}

	if volume < 0 || volume > 1000 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.VolumeOutOfRange))
		return nil
	}

	if err := player.SetVolume(i.GuildID, volume); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Music.VolumeSetFailed, err)))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T().Music.VolumeSetTitle, fmt.Sprintf(messages.T().Music.VolumeSetDesc, volume)))
	return nil
}

// HandleRepeat handles the repeat command
func HandleRepeat(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var mode int
	if len(options) > 0 {
		arg := options[0].StringValue()
		switch arg {
		case "on", "all":
			mode = queue.RepeatAll
		case "single":
			mode = queue.RepeatSingle
		default:
			mode = queue.RepeatOff
		}
	} else {
		// Cycle: Off → All → Single → Off
		currentMode, err := queue.GetRepeatMode(i.GuildID)
		if err != nil {
			logger.Errorf("[Repeat] Failed to get current repeat mode: %v", err)
			currentMode = queue.RepeatOff
		}
		switch currentMode {
		case queue.RepeatOff:
			mode = queue.RepeatAll
		case queue.RepeatAll:
			mode = queue.RepeatSingle
		case queue.RepeatSingle:
			mode = queue.RepeatOff
		}
	}

	if err := queue.SetRepeatMode(i.GuildID, mode); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Music.RepeatSetFailed, err)))
		return err
	}

	switch mode {
	case queue.RepeatAll:
		RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.TitleRepeatAll, messages.DescRepeatAll))
	case queue.RepeatSingle:
		RespondEmbed(s, i, messages.CreateInfoEmbed(messages.TitleRepeatSingle, messages.DescRepeatSingle))
	default:
		RespondEmbed(s, i, messages.CreateWarningEmbed(messages.TitleRepeatOff, messages.DescRepeatOff))
	}
	return nil
}

// HandleForceSkip handles the forceskip command (admin only)
func HandleForceSkip(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	DeferResponse(s, i)

	// Get queue
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoSong, messages.ErrorEmptyQueue))
		return nil
	}

	songTitle := q.Songs[0].Title
	songThumbnail := q.Songs[0].Thumbnail

	err = player.Skip(s, i.GuildID)
	if err != nil && err != player.ErrQueueEmpty {
		logger.Errorf("[ForceSkip] Failed to skip: %v", err)
		UpdateResponseEmbed(s, i, messages.CreateErrorEmbed(messages.T().Music.SkipFailedTitle, fmt.Sprintf(messages.T().Music.SkipFailedDesc, err)))
		return nil
	}

	if err == player.ErrQueueEmpty {
		embed := messages.CreateSuccessEmbed(messages.T().Music.PlaybackEndedTitle,
			fmt.Sprintf(messages.T().Music.ForceSkippedEnded, messages.EscapeMarkdown(songTitle)))
		messages.SetThumbnail(embed, songThumbnail)
		UpdateResponseEmbed(s, i, embed)

		if stopErr := player.Stop(i.GuildID); stopErr != nil {
			logger.Errorf("[ForceSkip] Failed to cleanup after queue empty: %v", stopErr)
		}
		return nil
	}

	embed := messages.CreateSuccessEmbed(messages.TitleSkipped,
		fmt.Sprintf(messages.T().Music.ForceSkipped, messages.EscapeMarkdown(songTitle)))
	messages.SetThumbnail(embed, songThumbnail)
	UpdateResponseEmbed(s, i, embed)
	return nil
}

// createProgressBar creates a visual progress bar
func createProgressBar(currentMs int, durationStr string) string {
	totalSeconds := youtube.ParseDurationToSeconds(durationStr)
	if totalSeconds == 0 {
		return "▬▬▬▬▬▬▬▬▬▬"
	}

	currentSeconds := currentMs / 1000
	progress := float64(currentSeconds) / float64(totalSeconds)
	if progress > 1.0 {
		progress = 1.0
	}

	barLength := 10
	filled := int(progress * float64(barLength))

	bar := ""
	for i := 0; i < barLength; i++ {
		if i < filled {
			bar += "▬"
		} else if i == filled {
			bar += "🔘"
		} else {
			bar += "▬"
		}
	}

	return bar
}

// boolToEmoji converts a boolean to emoji
func boolToEmoji(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}

// handlePlaylistConfirmationReaction handles the reaction interaction for playlist confirmation
func handlePlaylistConfirmationReaction(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistInfo *youtube.PlaylistInfo, voiceState *discordgo.VoiceState) {
	logger.Debugf("[PlaylistReaction] Starting reaction handler for message %s in channel %s", msg.ID, msg.ChannelID)
	logger.Debugf("[PlaylistReaction] Expecting reaction from user: %s", originalInteraction.Member.User.ID)

	confirmedChan := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		logger.Debugf("[PlaylistReaction] Received reaction: emoji=%s, messageID=%s, userID=%s", r.Emoji.Name, r.MessageID, r.UserID)

		// Ignore bot's own reactions
		if r.UserID == s.State.User.ID {
			logger.Debugf("[PlaylistReaction] Ignoring bot's own reaction")
			return
		}

		// Check if this is the correct message
		if r.MessageID != msg.ID {
			logger.Debugf("[PlaylistReaction] Message ID mismatch: expected %s, got %s", msg.ID, r.MessageID)
			return
		}

		// Check if correct emoji
		if r.Emoji.Name != "✅" {
			logger.Debugf("[PlaylistReaction] Emoji mismatch: expected ✅, got %s", r.Emoji.Name)
			return
		}

		// Only allow the original requester to confirm
		if r.UserID != originalInteraction.Member.User.ID {
			logger.Debugf("[PlaylistReaction] User ID mismatch: expected %s, got %s", originalInteraction.Member.User.ID, r.UserID)
			return
		}

		logger.Infof("[PlaylistReaction] Confirmed by user %s", r.UserID)

		// Signal confirmation (non-blocking)
		select {
		case confirmedChan <- true:
		default:
		}

		// Show loading message
		loadingEmbed := messages.CreateWarningEmbed(messages.T().Music.PlaylistAddingTitle, messages.T().Music.PlaylistAddingAll)

		// Edit the message - use direct message edit for text commands
		if isMessageCommand(originalInteraction) {
			s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, loadingEmbed)
		} else {
			s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
				Embeds: &[]*discordgo.MessageEmbed{loadingEmbed},
			})
		}

		// Remove all reactions
		s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)

		// Add playlist songs in background
		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceState, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)
	defer removeHandler()

	// Wait for either confirmation or timeout
	select {
	case <-confirmedChan:
		logger.Debugf("[PlaylistReaction] Reaction confirmed, handler complete")
		// Reaction confirmed, handler already processed it
	case <-time.After(30 * time.Second):
		logger.Debugf("[PlaylistReaction] Timeout reached, cancelling")
		// Timeout - update message and remove reactions
		embed := messages.CreateWarningEmbed(messages.T().Music.PlaylistTimeoutTitle, messages.T().Music.PlaylistTimeoutDesc)
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

// handlePlaylistRestConfirmationReaction handles adding the rest of a playlist via reactions
func handlePlaylistRestConfirmationReaction(s *discordgo.Session, originalInteraction *discordgo.InteractionCreate, msg *discordgo.Message, playlistID, videoID string, voiceState *discordgo.VoiceState) {
	logger.Debugf("[PlaylistRestReaction] Starting reaction handler for message %s in channel %s", msg.ID, msg.ChannelID)
	logger.Debugf("[PlaylistRestReaction] Expecting reaction from user: %s", originalInteraction.Member.User.ID)

	confirmedChan := make(chan bool, 1)

	reactionHandler := func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		logger.Debugf("[PlaylistRestReaction] Received reaction: emoji=%s, messageID=%s, userID=%s", r.Emoji.Name, r.MessageID, r.UserID)

		// Ignore bot's own reactions
		if r.UserID == s.State.User.ID {
			logger.Debugf("[PlaylistRestReaction] Ignoring bot's own reaction")
			return
		}

		// Check if this is the correct message
		if r.MessageID != msg.ID {
			logger.Debugf("[PlaylistRestReaction] Message ID mismatch: expected %s, got %s", msg.ID, r.MessageID)
			return
		}

		// Check if correct emoji
		if r.Emoji.Name != "⬇️" {
			logger.Debugf("[PlaylistRestReaction] Emoji mismatch: expected ⬇️, got %s", r.Emoji.Name)
			return
		}

		// Only allow the original requester to confirm
		if r.UserID != originalInteraction.Member.User.ID {
			logger.Debugf("[PlaylistRestReaction] User ID mismatch: expected %s, got %s", originalInteraction.Member.User.ID, r.UserID)
			return
		}

		logger.Infof("[PlaylistRestReaction] Confirmed by user %s", r.UserID)

		// Signal confirmation (non-blocking)
		select {
		case confirmedChan <- true:
		default:
		}

		// Fetch full playlist
		playlistURL := fmt.Sprintf("https://www.youtube.com/playlist?list=%s", playlistID)
		playlistInfo, err := youtube.GetPlaylistInfo(playlistURL, originalInteraction.Member.User.Username, originalInteraction.Member.User.ID)
		if err != nil {
			errorEmbed := messages.CreateErrorEmbed(messages.TitleError, messages.T().Music.PlaylistInfoFailed)
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

		// Filter out the current video (only if videoID is not empty)
		if videoID != "" {
			filteredVideos := make([]*youtube.Song, 0)
			for _, video := range playlistInfo.Videos {
				if !strings.Contains(video.URL, videoID) {
					filteredVideos = append(filteredVideos, video)
				}
			}
			playlistInfo.Videos = filteredVideos
		}

		// Show loading message
		loadingEmbed := messages.CreateWarningEmbed(messages.T().Music.PlaylistAddingTitle, messages.T().Music.PlaylistAddingRest)

		if isMessageCommand(originalInteraction) {
			s.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, loadingEmbed)
		} else {
			s.InteractionResponseEdit(originalInteraction.Interaction, &discordgo.WebhookEdit{
				Embeds: &[]*discordgo.MessageEmbed{loadingEmbed},
			})
		}

		// Remove all reactions
		s.MessageReactionsRemoveAll(msg.ChannelID, msg.ID)

		// Add remaining playlist songs
		go addPlaylistSongs(s, originalInteraction, playlistInfo, voiceState, msg.ID)
	}

	removeHandler := s.AddHandler(reactionHandler)
	defer removeHandler()

	// Wait for either confirmation or timeout
	select {
	case <-confirmedChan:
		logger.Debugf("[PlaylistRestReaction] Reaction confirmed, handler complete")
		// Reaction confirmed, handler already processed it
	case <-time.After(30 * time.Second):
		logger.Debugf("[PlaylistRestReaction] Timeout reached, cancelling")
		// Timeout - update message and remove reactions
		embed := messages.CreateWarningEmbed(messages.T().Music.PlaylistTimeoutTitle, messages.T().Music.PlaylistTimeoutDesc)
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

// addPlaylistSongs adds all songs from a playlist to the queue
// messageID is the Discord message ID to update with completion status (for text commands)
func addPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, playlistInfo *youtube.PlaylistInfo, voiceState *discordgo.VoiceState, messageID string) {
	// Acquire playlist lock to prevent order mixing when multiple playlists are added
	lock := getPlaylistLock(i.GuildID)
	lock.Lock()
	defer lock.Unlock()

	startTime := time.Now()
	logger.Infof("[Playlist] Starting playlist processing for %d songs", len(playlistInfo.Videos))

	// Check if queue is empty before adding
	q, _ := queue.GetQueue(i.GuildID, false)
	isQueueEmpty := q == nil || len(q.Songs) == 0

	// Create queue if it doesn't exist
	if q == nil {
		if err := queue.CreateQueue(i.GuildID, i.ChannelID, voiceState.ChannelID); err != nil {
			logger.Errorf("[Playlist] Failed to create queue: %v", err)
			return
		}
	} else {
		// Update voice channel to user's current channel (handles bot restart / channel switch)
		queue.UpdateVoiceChannel(i.GuildID, voiceState.ChannelID)
	}

	// Fast-track first playlist: process first song immediately, others synchronously (lock is held)
	if isQueueEmpty && len(playlistInfo.Videos) > 0 {
		logger.Info("[Playlist] Fast-tracking first song for immediate playback")

		addedCount, skippedCount := fastTrackFirstSong(i.GuildID, playlistInfo.Videos, s, i)

		initialTime := time.Since(startTime)
		logger.Infof("[Playlist] First song processed in %dms: %d added, %d skipped",
			initialTime.Milliseconds(), addedCount, skippedCount)

		// Start playing first song immediately (async is OK here)
		if addedCount > 0 {
			go player.Play(s, i.GuildID)
		}

		// Process remaining songs synchronously to maintain order (lock is still held)
		if len(playlistInfo.Videos) > 1 && addedCount > 0 {
			remainingSongs := playlistInfo.Videos[1:]
			logger.Infof("[Playlist] Processing remaining %d songs (synchronously to maintain order)", len(remainingSongs))

			processRemainingPlaylistSongs(s, i, remainingSongs, playlistInfo, startTime, messageID)
		}

		return
	}

	// Standard processing for non-first playlists (synchronously to maintain order)
	processAllPlaylistSongs(s, i, playlistInfo.Videos, playlistInfo, startTime, messageID)
}

// fastTrackFirstSong processes the first available song immediately (tries up to 3)
func fastTrackFirstSong(guildID string, songs []*youtube.Song, s *discordgo.Session, i *discordgo.InteractionCreate) (addedCount, skippedCount int) {
	// Try up to 3 songs to find an available one
	maxAttempts := 3
	if len(songs) < maxAttempts {
		maxAttempts = len(songs)
	}

	for idx := 0; idx < maxAttempts; idx++ {
		song := songs[idx]

		// Quick availability check
		available, isLive, err := youtube.CheckAvailability(song.URL)
		if err != nil || !available {
			logger.Debugf("[Playlist] Skipping unavailable video: %s - %v", song.Title, err)
			skippedCount++
			continue
		}

		// Update live status if detected
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
		logger.Infof("[Playlist] First song added: %s", song.Title)
		break
	}

	return addedCount, skippedCount
}

// processRemainingPlaylistSongs processes remaining songs in background with worker pool
func processRemainingPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Infof("[Playlist Background] Processing %d remaining songs with worker pool", len(songs))

	workerPool := worker.GetWorkerPool()

	// Prepare jobs for availability checking
	jobs := make([]worker.AvailabilityJob, 0, len(songs))
	for idx, song := range songs {
		jobs = append(jobs, worker.AvailabilityJob{
			URL:   song.URL,
			Index: idx,
		})
	}

	// Check all videos in parallel
	results := workerPool.CheckBatch(jobs)

	addedCount := 0
	skippedCount := 0

	// Collect skipped songs for later notification
	var skippedSongs []struct {
		Title     string
		Thumbnail string
		Error     string
	}

	// Add songs to queue in order
	for _, result := range results {
		song := songs[result.Index]

		// Only skip on definitive unavailable errors (geo, private, deleted, age-restricted)
		// Generic "unavailable" errors might be false negatives, so we add them anyway
		if !result.Available && isDefinitiveUnavailableError(result.Error) {
			logger.Debugf("[Playlist Background] Skipping definitively unavailable: %s - %s",
				song.Title, result.Error)
			skippedCount++
			skippedSongs = append(skippedSongs, struct {
				Title     string
				Thumbnail string
				Error     string
			}{Title: song.Title, Thumbnail: song.Thumbnail, Error: result.Error})
			continue
		}

		// Update live status if detected
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
	logger.Infof("[Playlist Background] Completed: %d added, %d skipped in %dms total",
		addedCount, skippedCount, totalTime.Milliseconds())

	// Send completion message (skip if shutting down)
	if shutdown.IsShuttingDown() {
		logger.Debug("[Playlist Background] Skipping completion message - bot is shutting down")
		return
	}

	description := fmt.Sprintf(messages.T().Music.PlaylistCompleteDesc,
		messages.EscapeMarkdown(playlistInfo.Title), playlistInfo.URL)

	if skippedCount > 0 {
		description += fmt.Sprintf("\n\n"+messages.T().Music.PlaylistSkippedCount, skippedCount)
	}

	successEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.TitlePlaylistAdded,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.FieldTotalSongs, Value: fmt.Sprintf(messages.T().Music.PlaylistSongsUnit, playlistInfo.VideoCount), Inline: true},
			{Name: messages.T().Music.PlaylistAddedCount, Value: fmt.Sprintf(messages.T().Music.PlaylistSongsUnit, addedCount+1), Inline: true},
			{Name: messages.FieldRequester, Value: i.Member.User.Username, Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
	}

	// Use ChannelMessageEdit for text commands, InteractionResponseEdit for slash commands
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

	// Send individual error messages for skipped songs (like NodeJS)
	for _, skipped := range skippedSongs {
		errorEmbed := &discordgo.MessageEmbed{
			Color:       messages.ColorError,
			Title:       messages.TitleUnavailable,
			Description: fmt.Sprintf("**%s**\n%s", messages.EscapeMarkdown(skipped.Title), cleanErrorMessage(skipped.Error)),
		}
		if skipped.Thumbnail != "" {
			errorEmbed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: skipped.Thumbnail}
		}
		if _, err := s.ChannelMessageSendEmbed(i.ChannelID, errorEmbed); err != nil {
			logger.Errorf("[Playlist Background] Failed to send skipped song notification: %v", err)
		}
	}
}

// processAllPlaylistSongs processes all songs (for non-first playlists)
func processAllPlaylistSongs(s *discordgo.Session, i *discordgo.InteractionCreate, songs []*youtube.Song, playlistInfo *youtube.PlaylistInfo, startTime time.Time, messageID string) {
	logger.Infof("[Playlist] Standard processing for %d songs", len(songs))

	workerPool := worker.GetWorkerPool()

	// Prepare jobs
	jobs := make([]worker.AvailabilityJob, 0, len(songs))
	for idx, song := range songs {
		jobs = append(jobs, worker.AvailabilityJob{
			URL:   song.URL,
			Index: idx,
		})
	}

	// Check all in parallel
	results := workerPool.CheckBatch(jobs)
	checkTime := time.Since(startTime)
	logger.Infof("[Playlist] Availability check completed in %dms", checkTime.Milliseconds())

	addedCount := 0
	skippedCount := 0

	// Collect skipped songs for later notification
	var skippedSongs []struct {
		Title     string
		Thumbnail string
		Error     string
	}

	// Add songs in order
	for _, result := range results {
		song := songs[result.Index]

		// Only skip on definitive unavailable errors (geo, private, deleted, age-restricted)
		// Generic "unavailable" errors might be false negatives, so we add them anyway
		if !result.Available && isDefinitiveUnavailableError(result.Error) {
			logger.Debugf("[Playlist] Skipping definitively unavailable: %s - %s",
				song.Title, result.Error)
			skippedCount++
			skippedSongs = append(skippedSongs, struct {
				Title     string
				Thumbnail string
				Error     string
			}{Title: song.Title, Thumbnail: song.Thumbnail, Error: result.Error})
			continue
		}

		// Update live status if detected
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
	logger.Infof("[Playlist] Completed: %d added, %d skipped in %dms", addedCount, skippedCount, totalTime.Milliseconds())

	// Skip sending messages and starting playback if shutting down
	if shutdown.IsShuttingDown() {
		logger.Debug("[Playlist] Skipping completion message - bot is shutting down")
		return
	}

	// Send completion message
	description := fmt.Sprintf("**[%s](%s)**", messages.EscapeMarkdown(playlistInfo.Title), playlistInfo.URL)
	if skippedCount > 0 {
		description += fmt.Sprintf("\n\n"+messages.T().Music.PlaylistSkippedOrDup, skippedCount)
	}

	successEmbed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.TitlePlaylistAdded,
		Description: description,
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.T().Music.PlaylistAddedSongs, Value: fmt.Sprintf(messages.T().Music.PlaylistSongsUnit, addedCount), Inline: true},
			{Name: messages.FieldRequester, Value: i.Member.User.Username, Inline: true},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: playlistInfo.ThumbnailURL},
	}

	// Use ChannelMessageEdit for text commands, InteractionResponseEdit for slash commands
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

	// Send individual error messages for skipped songs (like NodeJS)
	for _, skipped := range skippedSongs {
		errorEmbed := &discordgo.MessageEmbed{
			Color:       messages.ColorError,
			Title:       messages.TitleUnavailable,
			Description: fmt.Sprintf("**%s**\n%s", messages.EscapeMarkdown(skipped.Title), cleanErrorMessage(skipped.Error)),
		}
		if skipped.Thumbnail != "" {
			errorEmbed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: skipped.Thumbnail}
		}
		if _, err := s.ChannelMessageSendEmbed(i.ChannelID, errorEmbed); err != nil {
			logger.Errorf("[Playlist] Failed to send skipped song notification: %v", err)
		}
	}

	// Start playing if not already playing (fix for songs not starting immediately)
	p := player.GetPlayer(i.GuildID)
	if !p.Playing && !p.Loading {
		q, _ := queue.GetQueue(i.GuildID, true)
		if q != nil && len(q.Songs) > 0 {
			logger.Infof("[Playlist] Starting playback after playlist addition")
			go player.Play(s, i.GuildID)
		}
	}
}
