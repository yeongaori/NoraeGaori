package player

import (
	"sync"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"

	"github.com/bwmarrin/discordgo"
)

func SetLoadingMessage(guildID string, msg *discordgo.Message) {
	clearAnnounced(guildID)

	loadingMessagesMu.Lock()
	defer loadingMessagesMu.Unlock()
	loadingMessages[guildID] = msg
	logger.Debugf("Stored loading message for guild: %s", guildID)
}

func GetLoadingMessage(guildID string) *discordgo.Message {
	loadingMessagesMu.RLock()
	defer loadingMessagesMu.RUnlock()
	return loadingMessages[guildID]
}

func DeleteLoadingMessage(guildID string) {
	loadingMessagesMu.Lock()
	defer loadingMessagesMu.Unlock()
	delete(loadingMessages, guildID)
	logger.Debugf("Deleted loading message for guild: %s", guildID)
}

type embedSender interface {
	ChannelMessageSendEmbed(channelID string, embed *discordgo.MessageEmbed, options ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageEditEmbed(channelID, messageID string, embed *discordgo.MessageEmbed, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

func sendNowPlayingMessage(session *discordgo.Session, guildID string, song *queue.Song, q *queue.Queue) {
	deliverNowPlaying(session, guildID, song, q)
}

func deliverNowPlaying(sender embedSender, guildID string, song *queue.Song, q *queue.Queue) {
	loadingMsg := GetLoadingMessage(guildID)
	if loadingMsg != nil {
		nowPlayingEmbed := messages.CreateSongEmbed(
			guildID,
			messages.ColorSuccess,
			messages.T(guildID).Player.PlaybackStarted,
			"",
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)

		_, err := sender.ChannelMessageEditEmbed(loadingMsg.ChannelID, loadingMsg.ID, nowPlayingEmbed)
		if err != nil {
			logger.Warnf("Failed to update loading message: %v", err)
			if q.ShowStartedTrack {
				if _, sendErr := sender.ChannelMessageSendEmbed(q.TextChannelID, nowPlayingEmbed); sendErr != nil {
					logger.Warnf("Failed to send the now playing message: %v", sendErr)
				}
			}
		}

		DeleteLoadingMessage(guildID)
	} else if q.ShowStartedTrack {
		embed := messages.CreateSongEmbed(
			guildID,
			messages.ColorSuccess,
			messages.T(guildID).Player.NowPlaying,
			"",
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)
		if _, err := sender.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
			logger.Warnf("Failed to send the now playing message: %v", err)
		}
	}

	player := messages.T(guildID).Player
	resolveNotice(sender, &reconnectNotices, guildID, song, player.StreamReconnectedTitle, player.StreamReconnectedDesc)
	resolveNotice(sender, &rateLimitNotices, guildID, song, player.RateLimitClearedTitle, player.RateLimitClearedDesc)
}

func resolveNotice(sender embedSender, notices *guildMessages, guildID string, song *queue.Song, title, description string) {
	notice := notices.get(guildID)
	if notice == nil {
		return
	}

	resolvedEmbed := messages.CreateSongEmbed(
		guildID,
		messages.ColorSuccess,
		title,
		description,
		song.Title,
		song.URL,
		song.Uploader,
		song.Duration,
		song.RequestedByTag,
		song.Thumbnail,
	)
	if _, err := sender.ChannelMessageEditEmbed(notice.ChannelID, notice.ID, resolvedEmbed); err != nil {
		logger.Warnf("Failed to update the %q notice: %v", title, err)
	}
	notices.remove(guildID)
}

type guildMessages struct {
	mu      sync.RWMutex
	byGuild map[string]*discordgo.Message
}

func (notices *guildMessages) set(guildID string, msg *discordgo.Message) {
	notices.mu.Lock()
	defer notices.mu.Unlock()
	if notices.byGuild == nil {
		notices.byGuild = make(map[string]*discordgo.Message)
	}
	notices.byGuild[guildID] = msg
}

func (notices *guildMessages) get(guildID string) *discordgo.Message {
	notices.mu.RLock()
	defer notices.mu.RUnlock()
	return notices.byGuild[guildID]
}

func (notices *guildMessages) remove(guildID string) {
	notices.mu.Lock()
	defer notices.mu.Unlock()
	delete(notices.byGuild, guildID)
}

func sendReconnectMessage(session *discordgo.Session, guildID string, song *queue.Song) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		return
	}

	embed := messages.CreateSongEmbed(
		guildID,
		messages.ColorWarning,
		messages.T(guildID).Player.StreamReconnectingTitle,
		messages.T(guildID).Player.StreamReconnectingDesc,
		song.Title,
		song.URL,
		song.Uploader,
		song.Duration,
		song.RequestedByTag,
		song.Thumbnail,
	)
	msg, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed)
	if err == nil && msg != nil {
		reconnectNotices.set(guildID, msg)
	}
}

func sendRateLimitMessage(session *discordgo.Session, guildID string, song *queue.Song) {
	postRateLimitNotice(session, guildID, song)
}

func postRateLimitNotice(sender embedSender, guildID string, song *queue.Song) {
	if rateLimitNotices.get(guildID) != nil {
		return
	}
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		return
	}

	embed := messages.CreateSongEmbed(
		guildID,
		messages.ColorWarning,
		messages.T(guildID).Player.RateLimitedTitle,
		messages.T(guildID).Player.RateLimitedDesc,
		song.Title,
		song.URL,
		song.Uploader,
		song.Duration,
		song.RequestedByTag,
		song.Thumbnail,
	)
	msg, err := sender.ChannelMessageSendEmbed(q.TextChannelID, embed)
	if err != nil || msg == nil {
		logger.Warnf("Failed to send the rate limit notice: %v", err)
		return
	}
	rateLimitNotices.set(guildID, msg)
}

func sendSongErrorMessage(session *discordgo.Session, guildID string, song *queue.Song, reason string) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		logger.Warnf("Cannot send error message - no text channel for guild: %s", guildID)
		return
	}

	embed := messages.CreateSongEmbed(
		guildID,
		messages.ColorError,
		messages.T(guildID).Player.PlaybackFailedTitle,
		reason,
		song.Title,
		song.URL,
		song.Uploader,
		song.Duration,
		song.RequestedByTag,
		song.Thumbnail,
	)
	session.ChannelMessageSendEmbed(q.TextChannelID, embed)
}

func sendPlaybackCrashMessage(session *discordgo.Session, guildID string, song *queue.Song) {
	sendSongErrorMessage(session, guildID, song, messages.T(guildID).Player.PlaybackCrashRetry)
}

func sendPlaybackEndMessage(session *discordgo.Session, guildID, reason string, isLeaving bool) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		logger.Debugf("Cannot send playback end message: no queue or text channel")
		return
	}

	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, playbackEndEmbed(guildID, reason, isLeaving)); err != nil {
		logger.Debugf("Failed to send playback end message: %v", err)
	}
}

func playbackEndEmbed(guildID, reason string, isLeaving bool) *discordgo.MessageEmbed {
	playerStrings := messages.T(guildID).Player
	description, footer, color := playerStrings.LeavingDefaultDesc, reason, messages.ColorInfo

	switch reason {
	case "empty":
		description, footer = playerStrings.LeavingEmptyDesc, playerStrings.LeavingEmptyFooter
		if !isLeaving {
			description, footer = playerStrings.QueueFinishedDesc, playerStrings.StayingFooter
		}
	case "error":
		description, footer, color = playerStrings.LeavingErrorDesc, playerStrings.LeavingErrorFooter, messages.ColorError
		if !isLeaving {
			footer = playerStrings.StayingFooter
		}
	}

	return &discordgo.MessageEmbed{
		Description: description,
		Color:       color,
		Footer:      &discordgo.MessageEmbedFooter{Text: footer},
		Timestamp:   time.Now().Format(time.RFC3339),
	}
}
