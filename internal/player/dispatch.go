package player

import (
	"fmt"
	"runtime/debug"

	"noraegaori/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func (player *GuildPlayer) processCommands() {
	defer func() {

		if r := recover(); r != nil {
			logger.Errorf("Panic recovered for guild %s: %v\n%s", player.GuildID, r, debug.Stack())
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
			player.noteCommand(cmd.Type)

			func() {
				var err error
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("command panic: %v", r)
						logger.Errorf("Command %s panicked for guild %s: %v\n%s", cmd.Type, player.GuildID, r, debug.Stack())
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
	case "leave":
		return leaveInternal(cmd.GuildID)
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
	player := GetPlayer(guildID)
	done := player.beginSession()
	go runPlaybackSession(session, player, done, release, playCurrentSong)

	return nil
}

func runPlaybackSession(session *discordgo.Session, player *GuildPlayer, done chan struct{}, release func(), playSong func(*discordgo.Session, string) playResult) {
	defer release()
	defer player.endSession(done)
	defer recoverPlaybackSession(session, player)

	for !player.isHalted() && playSong(session, player.GuildID) != playStop {
	}
}
