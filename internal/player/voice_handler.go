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
	autoPauseDelay = 3 * time.Second 
)

var (
	
	autoPauseTimers   = make(map[string]*time.Timer)
	autoPauseTimersMu sync.Mutex
)

func HandleVoiceStateUpdate(session *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	
	guild, err := session.State.Guild(vsu.GuildID)
	if err != nil {
		logger.Errorf("[VoiceHandler] Failed to get guild: %v", err)
		return
	}

	
	var botVoiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == session.State.User.ID {
			botVoiceChannelID = vs.ChannelID
			break
		}
	}

	
	if botVoiceChannelID == "" {
		return
	}

	
	if vsu.UserID == session.State.User.ID {
		player := GetPlayer(vsu.GuildID)
		if player != nil {
			player.mu.Lock()
			if player.VoiceChannelID != "" && player.VoiceChannelID != botVoiceChannelID {
				logger.Infof("[VoiceHandler] Bot was moved from %s to %s in guild: %s", player.VoiceChannelID, botVoiceChannelID, vsu.GuildID)
				player.VoiceChannelID = botVoiceChannelID
				player.mu.Unlock()
				if err := queue.UpdateVoiceChannel(vsu.GuildID, botVoiceChannelID); err != nil {
					logger.Errorf("[VoiceHandler] Failed to update queue voice channel: %v", err)
				}
			} else {
				player.mu.Unlock()
			}
		}
	}

	
	humanCount := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == botVoiceChannelID && vs.UserID != session.State.User.ID {
			
			user, err := session.User(vs.UserID)
			if err == nil && !user.Bot {
				humanCount++
			}
		}
	}

	if humanCount == 0 {
		
		logger.Debugf("[VoiceHandler] Voice channel empty for guild: %s, starting auto-pause timer", vsu.GuildID)
		startAutoPauseTimer(session, vsu.GuildID, botVoiceChannelID)
	} else {
		
		logger.Debugf("[VoiceHandler] Humans present in voice channel for guild: %s, canceling auto-pause", vsu.GuildID)
		cancelAutoPauseTimer(vsu.GuildID)
	}
}

func startAutoPauseTimer(session *discordgo.Session, guildID, channelID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	
	if timer, exists := autoPauseTimers[guildID]; exists {
		timer.Stop()
	}

	
	timer := time.AfterFunc(autoPauseDelay, func() {
		logger.Infof("[VoiceHandler] Auto-pausing playback for guild: %s", guildID)

		
		player := GetPlayer(guildID)
		if player == nil {
			return
		}

		
		player.mu.Lock()
		if !player.Playing {
			player.mu.Unlock()
			return
		}

		
		elapsed := time.Since(player.PlaybackStart)
		seekTime := int(elapsed.Milliseconds())

		
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

		
		q, err := queue.GetQueue(guildID, false)
		if err == nil && q != nil && len(q.Songs) > 0 {
			currentSong := q.Songs[0]
			_, err = queue.SaveSeekTime(guildID, currentSong.ID, seekTime)
			if err != nil {
				logger.Errorf("[VoiceHandler] Failed to save seek time: %v", err)
			}
		}

		
		if err := queue.SetPaused(guildID, true); err != nil {
			logger.Errorf("[VoiceHandler] Failed to set paused state: %v", err)
		}
		if err := queue.SetPlaying(guildID, false); err != nil {
			logger.Errorf("[VoiceHandler] Failed to clear playing state: %v", err)
		}

		
		player.mu.Lock()
		if player.VoiceConn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			player.VoiceConn.Disconnect(ctx)
			cancel()
			player.VoiceConn = nil
			player.VoiceChannelID = ""
		}
		player.mu.Unlock()

		logger.Debugf("[VoiceHandler] Auto-paused at %dms for guild: %s", seekTime, guildID)

		
		go sendAutoPauseNotification(session, guildID, channelID)

		
		autoPauseTimersMu.Lock()
		delete(autoPauseTimers, guildID)
		autoPauseTimersMu.Unlock()
	})

	autoPauseTimers[guildID] = timer
}

func cancelAutoPauseTimer(guildID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	if timer, exists := autoPauseTimers[guildID]; exists {
		timer.Stop()
		delete(autoPauseTimers, guildID)
		logger.Debugf("[VoiceHandler] Canceled auto-pause timer for guild: %s", guildID)
	}
}

func sendAutoPauseNotification(session *discordgo.Session, guildID, voiceChannelID string) {
	
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		logger.Errorf("[VoiceHandler] Failed to get queue for notification: %v", err)
		return
	}

	
	channel, err := session.Channel(voiceChannelID)
	if err != nil {
		logger.Errorf("[VoiceHandler] Failed to get voice channel: %v", err)
		return
	}

	
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       messages.T(guildID).VoiceHandler.AutoPauseTitle,
		Description: fmt.Sprintf(messages.T(guildID).VoiceHandler.AutoPauseDesc, channel.Name),
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	
	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
		logger.Errorf("[VoiceHandler] Failed to send auto-pause notification: %v", err)
	}
}

func ClearAutoPauseTimer(guildID string) {
	cancelAutoPauseTimer(guildID)
}
