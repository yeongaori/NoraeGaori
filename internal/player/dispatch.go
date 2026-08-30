package player

import (
	"fmt"
	"time"

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
		case cmd, ok := <-player.CommandChan:
			if !ok {

				logger.Debugf("CommandChan closed for guild: %s", player.GuildID)
				return
			}

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
		return playInternal(cmd.Session, cmd.GuildID)
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
	player := GetPlayer(guildID)

	cmd := PlayerCommand{
		Type:    "play",
		Session: session,
		GuildID: guildID,
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("Recovered from panic (channel likely closed) for guild %s: %v", guildID, r)
		}
	}()

	select {
	case player.CommandChan <- cmd:

		return nil
	default:

		logger.Warnf("Command queue full for guild %s", guildID)
		return fmt.Errorf("command queue full, please try again")
	}
}

func playInternal(session *discordgo.Session, guildID string) error {

	lock := acquirePlayLock(guildID)

	lockAcquired := make(chan bool, 1)
	unlockChan := make(chan struct{})

	go func() {
		lock.Lock()
		select {
		case lockAcquired <- true:

			<-unlockChan
			lock.Unlock()
		default:

			lock.Unlock()
		}
	}()

	select {
	case <-lockAcquired:

		defer close(unlockChan)
	case <-time.After(lockTimeout):
		logger.Warnf("Lock timeout for guild: %s", guildID)
		return fmt.Errorf("play lock timeout")
	}

	logger.Debugf("Lock acquired for guild: %s", guildID)

	for {
		result := playSingleSong(session, guildID)
		switch result {
		case playContinue:

			continue
		case playStop:

			return nil
		case playError:

			return fmt.Errorf("playback error")
		}
	}
}
