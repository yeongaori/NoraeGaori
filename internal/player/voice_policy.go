package player

import (
	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
)

func ShouldLeaveVoice(guildID string) bool {
	return isSettingEnabled(guildID, "auto-leave", queue.GetAutoLeave)
}

func shouldAutoPause(guildID string) bool {
	return isSettingEnabled(guildID, "auto-pause", queue.GetAutoPause)
}

func shouldAutoResume(guildID string) bool {
	return isSettingEnabled(guildID, "auto-resume", queue.GetAutoResume)
}

func isSettingEnabled(guildID, name string, get func(string) (bool, error)) bool {
	enabled, err := get(guildID)
	if err != nil {
		logger.Warnf("Failed to read the %s setting for guild %s, keeping it on: %v", name, guildID, err)
		return true
	}
	return enabled
}
