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

var (
	autoPauseDelay     = 3 * time.Second
	autoPauseTimers    = make(map[string]*time.Timer)
	autoPausedChannels = make(map[string]string)
	autoPauseTimersMu  sync.Mutex
)

func HandleVoiceStateUpdate(session *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	resumeForReturningListener(session, vsu)

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
		if vs.ChannelID == botVoiceChannelID && isHumanListener(session, vs.UserID) {
			humanCount++
		}
	}

	if humanCount == 0 {
		if !shouldAutoPause(vsu.GuildID) {
			logger.Debugf("Voice channel empty for guild: %s, auto-pause is off", vsu.GuildID)
			return
		}
		logger.Debugf("Voice channel empty for guild: %s, starting auto-pause timer", vsu.GuildID)
		startAutoPauseTimer(session, vsu.GuildID, botVoiceChannelID)
	} else {

		logger.Debugf("Humans present in voice channel for guild: %s, canceling auto-pause", vsu.GuildID)
		cancelAutoPauseTimer(vsu.GuildID)
	}
}

func isHumanListener(session *discordgo.Session, userID string) bool {
	if userID == session.State.User.ID {
		return false
	}
	user, err := session.User(userID)
	return err == nil && !user.Bot
}

func resumeForReturningListener(session *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	guildID := vsu.GuildID
	if vsu.ChannelID == "" || autoPausedChannel(guildID) != vsu.ChannelID {
		return
	}
	if !isHumanListener(session, vsu.UserID) || !shouldAutoResume(guildID) {
		return
	}
	if !claimAutoPause(guildID, vsu.ChannelID) || !hasPausedSongs(guildID) {
		return
	}

	if err := queue.UpdateVoiceChannel(guildID, vsu.ChannelID); err != nil {
		logger.Errorf("Failed to update queue voice channel before auto-resume: %v", err)
	}
	logger.Infof("Auto-resuming playback for guild: %s", guildID)
	resumeAutoPaused(session, guildID)
}

func hasPausedSongs(guildID string) bool {
	player := GetPlayer(guildID)
	player.mu.Lock()
	isPaused := player.Paused && !player.Playing && !player.Loading
	player.mu.Unlock()
	if !isPaused {
		return false
	}

	q, err := queue.GetQueue(guildID, false)
	return err == nil && q != nil && len(q.Songs) > 0
}

func autoPausedChannel(guildID string) string {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()
	return autoPausedChannels[guildID]
}

func claimAutoPause(guildID, channelID string) bool {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()

	if autoPausedChannels[guildID] != channelID {
		return false
	}
	delete(autoPausedChannels, guildID)
	return true
}

func forgetAutoPause(guildID string) {
	autoPauseTimersMu.Lock()
	defer autoPauseTimersMu.Unlock()
	delete(autoPausedChannels, guildID)
}

func pauseForEmptyChannel(session *discordgo.Session, guildID, channelID string) {
	if !shouldAutoPause(guildID) {
		logger.Debugf("Auto-pause was turned off before the timer fired for guild: %s", guildID)
		return
	}
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
	if err := pausePlayback(player); err != nil {
		logger.Errorf("Failed to leave voice during auto-pause: %v", err)
	}

	logger.Debugf("Auto-paused for guild: %s", guildID)

	go announceAutoPause(session, guildID, channelID)

	autoPauseTimersMu.Lock()
	delete(autoPauseTimers, guildID)
	autoPausedChannels[guildID] = channelID
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

	embed := autoPauseEmbed(guildID, channel.Name, shouldAutoResume(guildID))

	if _, err := session.ChannelMessageSendEmbed(q.TextChannelID, embed); err != nil {
		logger.Errorf("Failed to send auto-pause notification: %v", err)
	}
}

func autoPauseEmbed(guildID, channelName string, isResumingAutomatically bool) *discordgo.MessageEmbed {
	voiceStrings := messages.T(guildID).VoiceHandler
	description := voiceStrings.AutoPauseDesc
	if isResumingAutomatically {
		description = voiceStrings.AutoPauseResumeDesc
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorWarning,
		Title:       voiceStrings.AutoPauseTitle,
		Description: fmt.Sprintf(description, channelName),
		Timestamp:   time.Now().Format(time.RFC3339),
	}
}

func ClearAutoPauseTimer(guildID string) {
	cancelAutoPauseTimer(guildID)
}
