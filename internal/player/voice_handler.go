package player

import (
	"fmt"
	"sync"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"

	"github.com/bwmarrin/discordgo"
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
		logger.Errorf("Failed to get guild: %v", err)
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
				logger.Infof("Bot was moved from %s to %s in guild: %s", player.VoiceChannelID, botVoiceChannelID, vsu.GuildID)
				player.VoiceChannelID = botVoiceChannelID
				player.mu.Unlock()
				if err := queue.UpdateVoiceChannel(vsu.GuildID, botVoiceChannelID); err != nil {
					logger.Errorf("Failed to update queue voice channel: %v", err)
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

		logger.Debugf("Voice channel empty for guild: %s, starting auto-pause timer", vsu.GuildID)
		startAutoPauseTimer(session, vsu.GuildID, botVoiceChannelID)
	} else {

		logger.Debugf("Humans present in voice channel for guild: %s, canceling auto-pause", vsu.GuildID)
		cancelAutoPauseTimer(vsu.GuildID)
	}
}

func pauseForEmptyChannel(session *discordgo.Session, guildID, channelID string) {
	logger.Infof("Auto-pausing playback for guild: %s", guildID)

	player := GetPlayer(guildID)
	if player == nil {
		return
	}

	player.mu.Lock()
	isPlaying := player.Playing
	player.mu.Unlock()
	if !isPlaying {
		return
	}

	player.noteCommand("pause")
	if err := suspendPlayback(player); err != nil {
		logger.Errorf("Failed to leave voice during auto-pause: %v", err)
	}

	logger.Debugf("Auto-paused for guild: %s", guildID)

	go announceAutoPause(session, guildID, channelID)

	autoPauseTimersMu.Lock()
	delete(autoPauseTimers, guildID)
	autoPauseTimersMu.Unlock()
}

func startAutoPauseTimer(session *discordgo.Session, guildID, channelID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	if timer, exists := autoPauseTimers[guildID]; exists {
		timer.Stop()
	}

	timer := time.AfterFunc(autoPauseDelay, func() {
		pauseForEmptyChannel(session, guildID, channelID)
	})

	autoPauseTimers[guildID] = timer
}

func cancelAutoPauseTimer(guildID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	if timer, exists := autoPauseTimers[guildID]; exists {
		timer.Stop()
		delete(autoPauseTimers, guildID)
		logger.Debugf("Canceled auto-pause timer for guild: %s", guildID)
	}
}

func sendAutoPauseNotification(session *discordgo.Session, guildID, voiceChannelID string) {

	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		logger.Errorf("Failed to get queue for notification: %v", err)
		return
	}

	channel, err := session.Channel(voiceChannelID)
	if err != nil {
		logger.Errorf("Failed to get voice channel: %v", err)
		return
	}

	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       messages.T(guildID).VoiceHandler.AutoPauseTitle,
		Description: fmt.Sprintf(messages.T(guildID).VoiceHandler.AutoPauseDesc, channel.Name),
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
		logger.Errorf("Failed to send auto-pause notification: %v", err)
	}
}

func ClearAutoPauseTimer(guildID string) {
	cancelAutoPauseTimer(guildID)
}
