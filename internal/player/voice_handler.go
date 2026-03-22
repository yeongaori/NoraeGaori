package player

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

const (
	autoPauseDelay = 3 * time.Second // Delay before auto-pause
)

var (
	// Auto-pause timers
	autoPauseTimers   = make(map[string]*time.Timer)
	autoPauseTimersMu sync.Mutex
)

// HandleVoiceStateUpdate handles voice state updates for auto-pause
func HandleVoiceStateUpdate(session *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	// Get the guild
	guild, err := session.State.Guild(vsu.GuildID)
	if err != nil {
		logger.Errorf("[VoiceHandler] Failed to get guild: %v", err)
		return
	}

	// Find the bot's voice channel
	var botVoiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == session.State.User.ID {
			botVoiceChannelID = vs.ChannelID
			break
		}
	}

	// Bot is not in a voice channel
	if botVoiceChannelID == "" {
		return
	}

	// Detect if the bot was forcibly moved to a different channel
	if vsu.UserID == session.State.User.ID {
		player := GetPlayer(vsu.GuildID)
		if player != nil {
			player.mu.Lock()
			if player.VoiceChannelID != "" && player.VoiceChannelID != botVoiceChannelID {
				logger.Infof("[VoiceHandler] Bot was moved from %s to %s in guild: %s", player.VoiceChannelID, botVoiceChannelID, vsu.GuildID)
				player.VoiceChannelID = botVoiceChannelID
				voiceConn := player.VoiceConn
				player.mu.Unlock()
				if err := queue.UpdateVoiceChannel(vsu.GuildID, botVoiceChannelID); err != nil {
					logger.Errorf("[VoiceHandler] Failed to update queue voice channel: %v", err)
				}
				if voiceConn != nil {
					voiceConn.RekeyDAVE()
				}
			} else {
				player.mu.Unlock()
			}
		}
	}

	// Check if the bot's voice channel is now empty (only bot remains)
	humanCount := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == botVoiceChannelID && vs.UserID != session.State.User.ID {
			// Check if user is a bot
			user, err := session.User(vs.UserID)
			if err == nil && !user.Bot {
				humanCount++
			}
		}
	}

	if humanCount == 0 {
		// Voice channel is empty, start auto-pause timer
		logger.Debugf("[VoiceHandler] Voice channel empty for guild: %s, starting auto-pause timer", vsu.GuildID)
		startAutoPauseTimer(session, vsu.GuildID, botVoiceChannelID)
	} else {
		// Humans are present, cancel auto-pause timer
		logger.Debugf("[VoiceHandler] Humans present in voice channel for guild: %s, canceling auto-pause", vsu.GuildID)
		cancelAutoPauseTimer(vsu.GuildID)
	}
}

// startAutoPauseTimer starts a timer to auto-pause when channel is empty
func startAutoPauseTimer(session *discordgo.Session, guildID, channelID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	// Cancel existing timer
	if timer, exists := autoPauseTimers[guildID]; exists {
		timer.Stop()
	}

	// Create new timer
	timer := time.AfterFunc(autoPauseDelay, func() {
		logger.Infof("[VoiceHandler] Auto-pausing playback for guild: %s", guildID)

		// Get the player
		player := GetPlayer(guildID)
		if player == nil {
			return
		}

		// Check if actually playing (under lock)
		player.mu.Lock()
		if !player.Playing {
			player.mu.Unlock()
			return
		}

		// Calculate current position while we have the lock
		elapsed := time.Since(player.PlaybackStart)
		seekTime := int(elapsed.Milliseconds())

		// Close stop channel to broadcast termination to all goroutines
		select {
		case <-player.StopChan:
			logger.Debugf("[VoiceHandler] Stop signal already pending for auto-pause: %s", guildID)
		default:
			close(player.StopChan)
			logger.Debugf("[VoiceHandler] Stop signal sent for auto-pause: %s", guildID)
		}

		player.Playing = false
		player.Paused = true
		player.mu.Unlock()

		// Save seek time to database
		q, err := queue.GetQueue(guildID, false)
		if err == nil && q != nil && len(q.Songs) > 0 {
			currentSong := q.Songs[0]
			_, err = queue.SaveSeekTime(guildID, currentSong.ID, seekTime)
			if err != nil {
				logger.Errorf("[VoiceHandler] Failed to save seek time: %v", err)
			}
		}

		// Set paused state in database
		if err := queue.SetPaused(guildID, true); err != nil {
			logger.Errorf("[VoiceHandler] Failed to set paused state: %v", err)
		}
		if err := queue.SetPlaying(guildID, false); err != nil {
			logger.Errorf("[VoiceHandler] Failed to clear playing state: %v", err)
		}

		// Disconnect from voice
		player.mu.Lock()
		if player.VoiceConn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			player.VoiceConn.Disconnect(ctx)
			cancel()
			player.VoiceConn = nil
			player.VoiceChannelID = ""
		}
		player.mu.Unlock()

		logger.Infof("[VoiceHandler] Auto-paused at %dms for guild: %s", seekTime, guildID)

		// Send notification to text channel
		go sendAutoPauseNotification(session, guildID, channelID)

		// Remove timer from map
		autoPauseTimersMu.Lock()
		delete(autoPauseTimers, guildID)
		autoPauseTimersMu.Unlock()
	})

	autoPauseTimers[guildID] = timer
}

// cancelAutoPauseTimer cancels the auto-pause timer
func cancelAutoPauseTimer(guildID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	if timer, exists := autoPauseTimers[guildID]; exists {
		timer.Stop()
		delete(autoPauseTimers, guildID)
		logger.Debugf("[VoiceHandler] Canceled auto-pause timer for guild: %s", guildID)
	}
}

// sendAutoPauseNotification sends a notification about auto-pause
func sendAutoPauseNotification(session *discordgo.Session, guildID, voiceChannelID string) {
	// Get the queue to find text channel
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		logger.Errorf("[VoiceHandler] Failed to get queue for notification: %v", err)
		return
	}

	// Get voice channel name
	channel, err := session.Channel(voiceChannelID)
	if err != nil {
		logger.Errorf("[VoiceHandler] Failed to get voice channel: %v", err)
		return
	}

	// Create embed
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       messages.T().VoiceHandler.AutoPauseTitle,
		Description: fmt.Sprintf(messages.T().VoiceHandler.AutoPauseDesc, channel.Name),
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	// Send to text channel
	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
		logger.Errorf("[VoiceHandler] Failed to send auto-pause notification: %v", err)
	}
}

// ClearAutoPauseTimer clears the auto-pause timer for a guild
func ClearAutoPauseTimer(guildID string) {
	cancelAutoPauseTimer(guildID)
}
