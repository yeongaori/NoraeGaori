package player

import (
	"fmt"

	"noraegaori/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func (player *GuildPlayer) processCommands() {
	defer func() {

		if r := recover(); r != nil {
			logger.Errorf("Panic recovered for guild %s: %v", player.GuildID, r)
		}

		player.mu.Lock()
		player.processorRunning = false
		player.mu.Unlock()
		logger.Debugf("Stopped for guild: %s", player.GuildID)
	}()

	logger.Debugf("Started for guild: %s", player.GuildID)

	for {
		select {
		case cmd := <-player.CommandChan:
			logger.Debugf("Received %s command for guild: %s", cmd.Type, player.GuildID)

			func() {
				var err error
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("command panic: %v", r)
						logger.Errorf("Command %s panicked for guild %s: %v", cmd.Type, player.GuildID, r)
					}

					logger.Debugf("Command %s completed for guild %s with error: %v", cmd.Type, player.GuildID, err)

					if cmd.Done != nil {
						select {
						case cmd.Done <- err:
						default:
							logger.Warnf("Could not send result for %s command in guild %s", cmd.Type, player.GuildID)
						}
						close(cmd.Done)
					}
				}()

				handler := player.dispatch
				if handler == nil {
					handler = player.defaultDispatch
				}
				err = handler(cmd)
			}()

		case <-player.QuitChan:

			logger.Debugf("Quit signal received for guild: %s", player.GuildID)
			return
		}
	}
}

func (player *GuildPlayer) defaultDispatch(cmd PlayerCommand) error {
	switch cmd.Type {
	case "play":
		return startPlaybackSession(cmd.Session, cmd.GuildID)
	case "skip":
		logger.Debugf("Processing skip command for guild: %s", player.GuildID)
		return skipInternal(cmd.Session, cmd.GuildID)
	case "stop":
		return stopInternal(cmd.GuildID)
	case "pause":
		return pauseInternal(cmd.GuildID)
	case "resume":
		return resumeInternal(cmd.Session, cmd.GuildID)
	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

func Play(session *discordgo.Session, guildID string) error {
	return sendCommandToPlayer(guildID, PlayerCommand{
		Type:    "play",
		Session: session,
		GuildID: guildID,
	})
}

func startPlaybackSession(session *discordgo.Session, guildID string) error {
	release, isAcquired := playLocks.AcquireWithTimeout(guildID, playLockWait)
	if !isAcquired {
		logger.Debugf("Playback already active for guild: %s", guildID)
		return ErrPlaybackAlreadyActive
	}

	logger.Debugf("Lock acquired for guild: %s", guildID)
	go runPlaybackSession(session, guildID, release, playCurrentSong)

	return nil
}

func runPlaybackSession(session *discordgo.Session, guildID string, release func(), playSong func(*discordgo.Session, string) playResult) {
	defer release()
	defer recoverPlaybackSession(guildID)

	for playSong(session, guildID) != playStop {
	}
}

func recoverPlaybackSession(guildID string) {
	reason := recover()
	if reason == nil {
		return
	}

	logger.Errorf("Playback session panicked for guild %s: %v", guildID, reason)

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.Playing = false
	player.Loading = false
	player.mu.Unlock()
}
