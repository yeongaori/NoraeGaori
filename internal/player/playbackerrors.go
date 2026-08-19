package player

import (
	"errors"
	"strings"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"

	"github.com/bwmarrin/discordgo"
)

func isDefinitivePlaybackError(errMsg string) bool {
	errorLower := strings.ToLower(errMsg)
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

func cleanPlaybackErrorMessage(guildID, errMsg string) string {
	errorLower := strings.ToLower(errMsg)
	t := messages.T(guildID)
	errorMappings := map[string]string{
		"private video":                 t.Player.ErrorPrivateVideo,
		"deleted video":                 t.Player.ErrorDeletedVideo,
		"age-restricted":                t.Player.ErrorAgeRestricted,
		"age restricted":                t.Player.ErrorAgeRestricted,
		"not available in your country": t.Player.ErrorGeoRestricted,
		"geo":                           t.Player.ErrorGeoRestricted,
		"members-only":                  t.Player.ErrorMembersOnly,
		"members only":                  t.Player.ErrorMembersOnly,
		"premium":                       t.Player.ErrorPremiumOnly,
		"copyright":                     t.Player.ErrorCopyright,
		"blocked":                       t.Player.ErrorBlocked,
		"removed by the uploader":       t.Player.ErrorRemovedByUploader,
		"account associated with this video has been terminated": t.Player.ErrorAccountTerminated,
	}
	for pattern, message := range errorMappings {
		if strings.Contains(errorLower, pattern) {
			return message
		}
	}

	if strings.Contains(errorLower, "video unavailable") || strings.Contains(errorLower, "not available") {
		return t.Player.ErrorUnavailable
	}
	return t.Player.ErrorUnavailable
}

func retryKey(guildID, songURL string) string {
	return guildID + ":" + songURL
}

func TransitionPending(guildID string) bool {
	playersMu.Lock()
	player, exists := players[guildID]
	playersMu.Unlock()
	if !exists {
		return false
	}
	return player.transitionArmed.Load()
}

func markAnnounced(guildID string, songID int) bool {
	announcedSongsMu.Lock()
	defer announcedSongsMu.Unlock()
	if current, exists := announcedSongs[guildID]; exists && current == songID {
		return false
	}
	announcedSongs[guildID] = songID
	return true
}

func clearAnnounced(guildID string) {
	announcedSongsMu.Lock()
	defer announcedSongsMu.Unlock()
	delete(announcedSongs, guildID)
}

func resolveRestartStreamURL(guildID string, song *queue.Song, sponsorBlock bool, bitrate int, current string) (string, error) {
	if current != "" || song.IsLive {
		return current, nil
	}
	invalidatePreCacheSong(guildID, song.ID)
	return fetchStreamURL(song.URL, sponsorBlock, bitrate)
}

func clearRetryCount(guildID, songURL string) {
	playbackRetriesMu.Lock()
	delete(playbackRetries, retryKey(guildID, songURL))
	playbackRetriesMu.Unlock()
}

func clearRetryCountsForGuild(guildID string) {
	clearAnnounced(guildID)
	prefix := guildID + ":"
	playbackRetriesMu.Lock()
	for key := range playbackRetries {
		if strings.HasPrefix(key, prefix) {
			delete(playbackRetries, key)
		}
	}
	playbackRetriesMu.Unlock()
}

func isStreamFetchFailure(errMsg string) bool {
	errorLower := strings.ToLower(errMsg)
	return strings.Contains(errorLower, "produced no audio") || strings.Contains(errorLower, "403")
}

func reportPlaybackFailure(song *queue.Song, errMsg string) {
	if !isStreamFetchFailure(errMsg) {
		return
	}

	reportStreamFailure(song.URL, errors.New(errMsg))
}

var reportStreamFailure = youtube.SaveStreamFailure

func handlePlaybackError(session *discordgo.Session, guildID string, song *queue.Song, err error) bool {
	errMsg := err.Error()

	reportPlaybackFailure(song, errMsg)

	if isDefinitivePlaybackError(errMsg) {
		reason := cleanPlaybackErrorMessage(guildID, errMsg)
		logger.Warnf("Definitive error for song %s in guild %s: %s", song.Title, guildID, reason)
		song.SetState(queue.SongStateFailed)
		announceSongError(session, guildID, song, reason)
		clearRetryCount(guildID, song.URL)
		return false
	}

	if song.IsLive {
		if active, _ := youtube.IsLiveStreamActive(song.URL); active {
			logger.Warnf("Live stream still active, reconnecting in guild: %s - %s", guildID, song.Title)
			return true
		}
		logger.Debugf("Live stream ended, not retrying in guild: %s - %s", guildID, song.Title)
		clearRetryCount(guildID, song.URL)
		return false
	}

	key := retryKey(guildID, song.URL)
	playbackRetriesMu.Lock()
	retries := playbackRetries[key]
	retries++
	playbackRetries[key] = retries
	playbackRetriesMu.Unlock()

	if retries < maxRetries {
		logger.Warnf("Retrying song (attempt %d/%d) in guild: %s - %s", retries, maxRetries, guildID, song.Title)
		return true
	}

	song.SetState(queue.SongStateFailed)
	logger.Errorf("Max retries exceeded for song %s in guild: %s", song.Title, guildID)

	if reconnectMsg := getReconnectMessage(guildID); reconnectMsg != nil {
		failedEmbed := messages.CreateSongEmbed(
			guildID,
			messages.ColorError,
			messages.T(guildID).Player.StreamReconnectFailedTitle,
			messages.T(guildID).Player.StreamReconnectFailedDesc,
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)
		session.ChannelMessageEditEmbed(reconnectMsg.ChannelID, reconnectMsg.ID, failedEmbed)
		deleteReconnectMessage(guildID)
	} else {
		announceSongError(session, guildID, song, messages.T(guildID).Player.MaxRetriesSkipping)
	}

	clearRetryCount(guildID, song.URL)
	return false
}
