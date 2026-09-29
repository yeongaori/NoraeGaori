package player

import (
	"context"
	"errors"
	"strings"
	"sync"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"

	"github.com/bwmarrin/discordgo"
)

var (
	awaitingDiscord   = make(map[string]struct{})
	awaitingDiscordMu sync.Mutex

	resumeAfterOutage = ResumeOrStart
)

func isDiscordUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, discordgo.ErrWSNotFound) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "timeout waiting for voice") || isVoiceConnectionError(errMsg)
}

func markAwaitingDiscord(guildID string) {
	awaitingDiscordMu.Lock()
	defer awaitingDiscordMu.Unlock()
	awaitingDiscord[guildID] = struct{}{}
}

func forgetAwaitingDiscord(guildID string) {
	awaitingDiscordMu.Lock()
	defer awaitingDiscordMu.Unlock()
	delete(awaitingDiscord, guildID)
}

func takeAwaitingDiscord() []string {
	awaitingDiscordMu.Lock()
	defer awaitingDiscordMu.Unlock()

	guildIDs := make([]string, 0, len(awaitingDiscord))
	for guildID := range awaitingDiscord {
		guildIDs = append(guildIDs, guildID)
	}
	clear(awaitingDiscord)
	return guildIDs
}

func ResumeAfterReconnect(session *discordgo.Session) {
	for _, guildID := range takeAwaitingDiscord() {
		if !canResumeAfterOutage(guildID) {
			continue
		}
		logger.Infof("Resuming playback for guild %s after reconnecting to Discord", guildID)
		resumeAfterOutage(session, guildID)
	}
}

func canResumeAfterOutage(guildID string) bool {
	player := GetPlayer(guildID)
	player.mu.Lock()
	isPaused := player.Paused
	player.mu.Unlock()
	if isPaused {
		return false
	}

	q, err := queue.GetQueue(guildID, false)
	return err == nil && q != nil && len(q.Songs) > 0
}
