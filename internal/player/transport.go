package player

import (
	"errors"
	"fmt"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"

	"github.com/bwmarrin/discordgo"
)

func Pause(guildID string) error {
	logger.Debugf("Pause called for guild %s", guildID)
	player := GetPlayer(guildID)
	player.noteCommand("pause")

	player.mu.Lock()
	isActive := player.Playing || player.Loading
	player.mu.Unlock()
	if !isActive {
		return ErrNotPlaying
	}

	return pausePlayback(player)
}

func pauseInternal(guildID string) error {
	return Pause(guildID)
}

func Leave(guildID string) error {
	return sendCommandAndWait(PlayerCommand{Type: "leave", GuildID: guildID}, leaveCommandTimeout)
}

func leaveInternal(guildID string) error {
	forgetAutoPause(guildID)
	return suspendPlayback(GetPlayer(guildID))
}

func suspendPlayback(player *GuildPlayer) error {
	seekTime := haltPlayback(player)
	if err := LeaveVoice(player.GuildID); err != nil {
		return err
	}

	logger.Debugf("Suspended playback at %dms for guild: %s", seekTime, player.GuildID)
	return nil
}

func pausePlayback(player *GuildPlayer) error {
	if ShouldLeaveVoice(player.GuildID) {
		return suspendPlayback(player)
	}

	seekTime := haltPlayback(player)
	logger.Debugf("Paused playback at %dms in voice for guild: %s", seekTime, player.GuildID)
	return nil
}

func haltPlayback(player *GuildPlayer) int {
	guildID := player.GuildID

	player.mu.Lock()
	wasPlaying := player.Playing
	isActive := wasPlaying || player.Loading
	seekTime := int(time.Since(player.PlaybackStart).Milliseconds())
	player.haltLocked()
	if isActive {
		player.Paused = true
	}
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		pending.Stream.Stop()
	}
	player.waitForSession(sessionExitWait)

	if wasPlaying {
		saveCurrentSeekTime(guildID, seekTime)
	}
	if isActive {
		persistIdleState(guildID, true)
	}
	return seekTime
}

func saveCurrentSeekTime(guildID string, seekTime int) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return
	}
	if _, err := queue.SaveSeekTime(guildID, q.Songs[0].ID, seekTime); err != nil {
		logger.Errorf("Failed to save seek time: %v", err)
	}
}

func sendCommandAndWait(cmd PlayerCommand, timeout time.Duration) error {
	done := make(chan error, 1)
	cmd.Done = done

	if err := sendCommandToPlayer(cmd.GuildID, cmd); err != nil {
		return err
	}

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		logger.Warnf("%s command timed out for guild %s", cmd.Type, cmd.GuildID)
		return ErrCommandTimeout
	}
}

func Seek(guildID string, positionMs int) error {
	if positionMs < 0 {
		return fmt.Errorf("seek position cannot be negative")
	}
	player := GetPlayer(guildID)

	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return fmt.Errorf("no current song")
	}
	song := q.Songs[0]
	if song.IsLive {
		return fmt.Errorf("cannot seek a live stream")
	}

	player.mu.Lock()
	if !player.Playing {
		player.mu.Unlock()
		return ErrNotPlaying
	}
	fadingOut := player.FadingOut
	player.mu.Unlock()
	if fadingOut {
		logger.Debugf("Fade-out in progress, letting it finish for guild: %s", guildID)
		return nil
	}

	if fadeOut, err := queue.GetFadeOut(guildID); err == nil && fadeOut {
		seconds, err := queue.GetFadeOutDuration(guildID)
		if err != nil || seconds <= 0 {
			seconds = 1
		}
		rampVolumeDown(guildID, seconds)
	}

	player.mu.Lock()
	if !player.Playing {
		player.mu.Unlock()
		return ErrNotPlaying
	}
	song.SeekTime = positionMs
	player.Seeking = true
	player.SeekTargetMs = positionMs
	select {
	case <-player.StopChan:

	default:
		close(player.StopChan)
	}
	player.mu.Unlock()

	if _, err := queue.SaveSeekTime(guildID, song.ID, positionMs); err != nil {
		logger.Errorf("Failed to persist seek time: %v", err)
		return err
	}
	logger.Debugf("Set position to %dms for guild %s", positionMs, guildID)
	return nil
}

func RestartForNormalization(guildID string) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()

	if !player.Playing {
		return
	}

	player.TogglingNorm = true
	select {
	case <-player.StopChan:

	default:
		close(player.StopChan)
	}
	logger.Debugf("Signaled FFmpeg restart for guild: %s", guildID)
}

func Resume(session *discordgo.Session, guildID string) error {
	return sendCommandAndWait(PlayerCommand{Type: "resume", Session: session, GuildID: guildID}, resumeCommandTimeout)
}

func ResumeOrStart(session *discordgo.Session, guildID string) {
	player := GetPlayer(guildID)

	player.mu.Lock()
	paused := player.Paused
	active := player.Playing || player.Loading
	player.mu.Unlock()

	if active {
		return
	}

	clearAnnounced(guildID)

	if paused {
		logger.Debugf("Resuming playback for guild: %s", guildID)
		go func() {
			if err := Resume(session, guildID); errors.Is(err, ErrPlaybackAlreadyActive) {
				logger.Debugf("Playback already active for guild %s", guildID)
			} else if err != nil {
				logger.Warnf("Failed to resume playback for guild %s: %v", guildID, err)
			}
		}()
		return
	}

	logger.Debugf("Starting playback for guild: %s", guildID)
	go func() {
		if err := Play(session, guildID); errors.Is(err, ErrPlaybackAlreadyActive) {
			logger.Debugf("Playback already active for guild %s", guildID)
		} else if err != nil {
			logger.Warnf("Failed to start playback for guild %s: %v", guildID, err)
		}
	}()
}

func resumeInternal(session *discordgo.Session, guildID string) error {

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		song := q.Songs[0]

		if song.IsLive {
			active, err := youtube.IsLiveStreamActive(song.URL)
			if err != nil || !active {
				logger.Warnf("Live stream ended or unavailable: %s", song.Title)

				if err := queue.RemoveFirstSong(guildID); err != nil {
					logger.Errorf("Failed to remove ended live stream: %v", err)
				}

				player := GetPlayer(guildID)
				player.mu.Lock()
				player.Paused = false
				player.mu.Unlock()

				logger.Infof("Skipping to next song after ended live stream")
				return startPlaybackSession(session, guildID)
			}
		}
	}

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.Paused = false
	player.FadeInNext = true
	player.mu.Unlock()

	ClearAutoPauseTimer(guildID)

	logger.Debugf("Resuming playback for guild: %s", guildID)
	return startPlaybackSession(session, guildID)
}

func Skip(session *discordgo.Session, guildID string) error {
	logger.Debugf("Skip called for guild %s", guildID)
	player := GetPlayer(guildID)
	player.noteCommand("skip")

	player.mu.Lock()
	fadingOut := player.FadingOut
	player.mu.Unlock()
	if fadingOut {
		logger.Debugf("Fade-out in progress, letting it finish for guild: %s", guildID)
		return nil
	}

	rampVolumeBeforeStop(guildID)

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.PlaybackDone:
		default:
		}

		player.signalStopLocked()
	}
	player.mu.Unlock()

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		pending := player.PendingStream
		player.PendingStream = nil
		player.mu.Unlock()

		if pending != nil {
			pending.Stream.Stop()
		}
	}

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		q.Songs[0].ResetRetry()
		logger.Debugf("Removing song: %s", q.Songs[0].Title)
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("Failed to remove song: %v", err)
		return fmt.Errorf("failed to remove song: %w", err)
	}

	logger.Debugf("Skipped song for guild: %s", guildID)

	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		logger.Infof("Queue is empty after skip for guild: %s", guildID)
		return ErrQueueEmpty
	}

	startNextSongAsync(session, guildID)

	return nil
}

func SkipTo(session *discordgo.Session, guildID string) error {
	logger.Debugf("Called for guild %s", guildID)
	rampVolumeBeforeStop(guildID)
	player := GetPlayer(guildID)
	player.noteCommand("skipto")

	player.mu.Lock()
	wasPlaying := player.Playing
	wasLoading := player.Loading

	if wasPlaying || wasLoading {

		select {
		case <-player.PlaybackDone:
		default:
		}

		player.signalStopLocked()
	}
	player.mu.Unlock()

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		player.mu.Unlock()
	}

	logger.Debugf("Starting playback of target song for guild: %s", guildID)

	startNextSongAsync(session, guildID)

	return nil
}

func skipInternal(session *discordgo.Session, guildID string) error {
	logger.Debugf("Called for guild %s", guildID)
	player := GetPlayer(guildID)

	player.mu.Lock()
	fadingOut := player.FadingOut
	player.mu.Unlock()
	if fadingOut {
		logger.Debugf("Fade-out in progress, letting it finish for guild: %s", guildID)
		return nil
	}

	rampVolumeBeforeStop(guildID)
	player.mu.Lock()

	wasPlaying := player.Playing
	if wasPlaying {

		select {
		case <-player.PlaybackDone:
		default:
		}

		player.signalStopLocked()
	}

	player.mu.Unlock()

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("Playback terminated for guild: %s", guildID)
		case <-time.After(2 * time.Second):
			logger.Warnf("Timeout waiting for playback to terminate for guild: %s", guildID)
		}

		player.mu.Lock()
		player.Playing = false
		player.Loading = false
		pending := player.PendingStream
		player.PendingStream = nil
		player.mu.Unlock()

		if pending != nil {
			pending.Stream.Stop()
		}
	}

	q, err := queue.GetQueue(guildID, false)
	if err == nil && q != nil && len(q.Songs) > 0 {
		q.Songs[0].ResetRetry()
		logger.Debugf("Removing song: %s", q.Songs[0].Title)
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("Failed to remove song: %v", err)
		return fmt.Errorf("failed to remove song: %w", err)
	}

	logger.Debugf("Skipped song for guild: %s", guildID)

	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		logger.Debugf("Queue is empty after skip for guild: %s", guildID)
		return ErrQueueEmpty
	}

	startNextSongAsync(session, guildID)

	return nil
}

func Stop(guildID string) error {
	return stopPlayback(guildID, ShouldLeaveVoice(guildID))
}

func Teardown(guildID string) error {
	return stopPlayback(guildID, true)
}

func stopPlayback(guildID string, isLeaving bool) error {
	defer callOnPlaybackEnded(guildID)

	logger.Debugf("Stop called for guild %s", guildID)
	rampVolumeBeforeStop(guildID)
	player := GetPlayer(guildID)
	player.noteCommand("stop")

	player.mu.Lock()
	wasPlaying := player.Playing

	if wasPlaying || player.Loading {
		select {
		case <-player.PlaybackDone:
		default:
		}
	}

	player.haltLocked()
	player.Paused = false
	player.mu.Unlock()

	if wasPlaying {
		select {
		case <-player.PlaybackDone:
			logger.Debugf("Playback terminated for guild: %s", guildID)
		case <-time.After(5 * time.Second):
			logger.Warnf("Timeout waiting for playback to terminate for guild: %s", guildID)
		}
	}

	if err := releasePlayback(guildID, isLeaving); err != nil {
		logger.Errorf("Failed to release playback: %v", err)
	}
	return nil
}

func stopInternal(guildID string, isLeaving bool) error {
	defer callOnPlaybackEnded(guildID)

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.haltLocked()
	player.Paused = false
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		pending.Stream.Stop()
	}

	return releasePlayback(guildID, isLeaving)
}

func releasePlayback(guildID string, isLeaving bool) error {
	if isLeaving {
		if err := LeaveVoice(guildID); err != nil {
			logger.Errorf("Failed to leave voice: %v", err)
		}
	}

	deleteErr := queue.DeleteQueue(guildID)

	ClearPreCache(guildID)
	StopAnalysisBackfill(guildID)
	forgetAutoPause(guildID)
	forgetAwaitingDiscord(guildID)

	if isLeaving {
		DeletePlayer(guildID)
	}
	logger.Debugf("Stopped playback for guild: %s (left voice: %t)", guildID, isLeaving)

	if deleteErr != nil {
		return fmt.Errorf("failed to delete queue: %w", deleteErr)
	}
	return nil
}

func startNextSongAsync(session *discordgo.Session, guildID string) {
	resume := resumePlayback

	go func() {
		if IsPlaybackActive(guildID) {
			logger.Debugf("Play operation already in progress for guild %s, skipping redundant play call", guildID)
			return
		}

		if err := resume(session, guildID); err != nil {
			if errors.Is(err, ErrPlaybackAlreadyActive) {
				logger.Debugf("Playback already active for guild %s (expected during rapid skips)", guildID)
			} else {
				logger.Errorf("Failed to play next song: %v", err)
			}
		}
	}()
}
