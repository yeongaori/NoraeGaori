package voice

import (
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

var errQueueUpdate = errors.New("failed to update the queue voice channel")

var (
	isPlaybackActive = player.IsPlaybackActive
	suspendPlayback  = player.Leave
	joinVoice        = func(s *discordgo.Session, guildID, channelID string) error {
		_, err := player.JoinVoice(s, guildID, channelID)
		return err
	}
	resumePlayback = player.Resume
)

func moveToChannel(s *discordgo.Session, guildID, channelID string) error {
	wasActive := isPlaybackActive(guildID)

	if err := suspendPlayback(guildID); err != nil {
		return err
	}

	if err := joinVoice(s, guildID, channelID); err != nil {
		return err
	}

	if hasQueuedSongs(guildID) {
		if err := queue.UpdateVoiceChannel(guildID, channelID); err != nil {
			return fmt.Errorf("%w: %v", errQueueUpdate, err)
		}
	}

	if wasActive {
		go func() {
			if err := resumePlayback(s, guildID); err != nil {
				logger.Errorf("Failed to resume playback after moving channels in guild %s: %v", guildID, err)
			}
		}()
	}
	return nil
}

func botVoiceChannelID(s *discordgo.Session, guildID string) string {
	voiceState, err := s.State.VoiceState(guildID, s.State.User.ID)
	if err != nil {
		return ""
	}
	return voiceState.ChannelID
}
