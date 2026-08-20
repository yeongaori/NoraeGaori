package player

import (
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"

	"github.com/bwmarrin/discordgo"
)

func SetLoadingMessage(guildID string, msg *discordgo.Message) {
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

func sendNowPlayingMessage(session *discordgo.Session, guildID string, song *queue.Song, q *queue.Queue) {
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

		_, err := session.ChannelMessageEditEmbed(loadingMsg.ChannelID, loadingMsg.ID, nowPlayingEmbed)
		if err != nil {
			logger.Warnf("Failed to update loading message: %v", err)
			if q.ShowStartedTrack {
				session.ChannelMessageSendEmbed(q.TextChannelID, nowPlayingEmbed)
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
		session.ChannelMessageSendEmbed(q.TextChannelID, embed)
	}

	if reconnectMsg := getReconnectMessage(guildID); reconnectMsg != nil {
		reconnectedEmbed := messages.CreateSongEmbed(
			guildID,
			messages.ColorSuccess,
			messages.T(guildID).Player.StreamReconnectedTitle,
			messages.T(guildID).Player.StreamReconnectedDesc,
			song.Title,
			song.URL,
			song.Uploader,
			song.Duration,
			song.RequestedByTag,
			song.Thumbnail,
		)
		session.ChannelMessageEditEmbed(reconnectMsg.ChannelID, reconnectMsg.ID, reconnectedEmbed)
		deleteReconnectMessage(guildID)
	}
}

func setReconnectMessage(guildID string, msg *discordgo.Message) {
	reconnectMessagesMu.Lock()
	defer reconnectMessagesMu.Unlock()
	reconnectMessages[guildID] = msg
}

func getReconnectMessage(guildID string) *discordgo.Message {
	reconnectMessagesMu.RLock()
	defer reconnectMessagesMu.RUnlock()
	return reconnectMessages[guildID]
}

func deleteReconnectMessage(guildID string) {
	reconnectMessagesMu.Lock()
	defer reconnectMessagesMu.Unlock()
	delete(reconnectMessages, guildID)
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
		setReconnectMessage(guildID, msg)
	}
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

func sendLeavingMessage(session *discordgo.Session, guildID, reason string) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || q.TextChannelID == "" {
		logger.Debugf("Cannot send leaving message: no queue or text channel")
		return
	}

	var embed *discordgo.MessageEmbed

	switch reason {
	case "empty":
		embed = &discordgo.MessageEmbed{
			Description: messages.T(guildID).Player.LeavingEmptyDesc,
			Color:       messages.ColorInfo,
			Footer: &discordgo.MessageEmbedFooter{
				Text: messages.T(guildID).Player.LeavingEmptyFooter,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	case "error":
		embed = &discordgo.MessageEmbed{
			Description: messages.T(guildID).Player.LeavingErrorDesc,
			Color:       messages.ColorError,
			Footer: &discordgo.MessageEmbedFooter{
				Text: messages.T(guildID).Player.LeavingErrorFooter,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	default:
		embed = &discordgo.MessageEmbed{
			Description: messages.T(guildID).Player.LeavingDefaultDesc,
			Color:       messages.ColorInfo,
			Footer: &discordgo.MessageEmbedFooter{
				Text: reason,
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
	}

	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
		logger.Debugf("Failed to send leaving message: %v", err)
	}
}
