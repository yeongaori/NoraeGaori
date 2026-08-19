package player

import (
	"context"
	"strings"
	"sync"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"

	"github.com/bwmarrin/discordgo"
)

type playResult int

const (
	playContinue playResult = iota
	playStop
	playError
)

func prepareVoiceConnection(session *discordgo.Session, player *GuildPlayer, guildID, voiceChannelID string) error {
	needsReconnect := player.VoiceConn == nil
	if player.VoiceConn != nil {
		select {
		case <-player.VoiceConn.DeadChan():
			logger.Warnf("Detected dead voice connection, will reconnect for guild: %s", guildID)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			player.VoiceConn.Disconnect(ctx)
			cancel()
			player.VoiceConn = nil
			needsReconnect = true
		default:
		}
	}

	if !needsReconnect {
		return nil
	}

	vc, err := joinVoiceChannel(session, guildID, voiceChannelID)
	if err != nil {
		return err
	}

	player.VoiceConn = vc
	logger.Debugf("Voice connection established for guild: %s", guildID)
	return nil
}

func markPlayerLoading(player *GuildPlayer, guildID string) {
	player.mu.Lock()
	player.Loading = true
	player.Playing = false
	player.Paused = false
	player.AutoMixAdvanced = false
	player.mu.Unlock()

	if err := queue.SetPaused(guildID, false); err != nil {
		logger.Errorf("Failed to clear paused state: %v", err)
	}
	if err := queue.SetLoading(guildID, true); err != nil {
		logger.Errorf("Failed to set loading state: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("Failed to set playing state: %v", err)
	}
}

func resolveSongStreamURL(player *GuildPlayer, song *queue.Song, guildID string, sponsorBlock bool, bitrate int) (string, bool, error) {
	player.mu.Lock()
	hasPending := player.PendingStream != nil && player.PendingStream.SongID == song.ID && song.SeekTime == 0
	player.mu.Unlock()

	if song.IsLive {
		logger.Debugf("Live stream, will stream via yt-dlp pipe for: %s", song.Title)
		return "", hasPending, nil
	}

	if hasPending {
		logger.Debugf("Using handed-off stream for: %s", song.Title)
		return GetCachedStreamURL(guildID, song.ID), hasPending, nil
	}

	if cached := GetCachedStreamURL(guildID, song.ID); cached != "" {
		logger.Debugf("Using pre-cached stream URL for: %s", song.Title)
		return cached, hasPending, nil
	}

	streamURL, err := fetchStreamURL(song.URL, sponsorBlock, bitrate)
	return streamURL, hasPending, err
}

func announceOnFirstFrame(session *discordgo.Session, player *GuildPlayer, song *queue.Song, q *queue.Queue, guildID string, hasPending bool, firstFrameCh <-chan struct{}, abortCh <-chan struct{}) {
	for {
		player.mu.Lock()
		stopCh := player.StopChan
		player.mu.Unlock()

		select {
		case <-firstFrameCh:
			if !hasPending && markAnnounced(guildID, song.ID) {
				announceNowPlaying(session, guildID, song, q)
			}
			return
		case <-abortCh:
			return
		case <-stopCh:
			player.mu.Lock()
			restarting := player.Seeking || player.TogglingNorm || player.StopChan != stopCh
			player.mu.Unlock()
			if restarting {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			dismissLoadingMessage(session, guildID)
			return
		}
	}
}

func repeatCopyOf(song *queue.Song) *queue.Song {
	return &queue.Song{
		URL:                song.URL,
		Title:              song.Title,
		Duration:           song.Duration,
		Thumbnail:          song.Thumbnail,
		Uploader:           song.Uploader,
		RequestedByID:      song.RequestedByID,
		RequestedByTag:     song.RequestedByTag,
		IsLive:             song.IsLive,
		AutoMixStyleVolume: song.AutoMixStyleVolume,
		AutoMixStyleEQ:     song.AutoMixStyleEQ,
		AutoMixStyleFilter: song.AutoMixStyleFilter,
		AutoMixStyleEffect: song.AutoMixStyleEffect,
		AutoMixStyleLoop:   song.AutoMixStyleLoop,
	}
}

func advanceQueueAfterSong(guildID string, song *queue.Song) {
	finalQueue, err := queue.GetQueue(guildID, false)
	if err != nil {
		logger.Errorf("Failed to reload queue for repeat check: %v", err)
	}

	repeatMode := queue.RepeatOff
	if finalQueue != nil {
		repeatMode = finalQueue.RepeatMode
	}

	shouldRepeat := repeatMode != queue.RepeatOff && !song.IsLive
	var repeatSong *queue.Song
	if shouldRepeat {
		repeatSong = repeatCopyOf(song)
	} else {
		logger.Debugf("Repeat check: queue=%v, repeatMode=%d, song.IsLive=%v", finalQueue != nil, repeatMode, song.IsLive)
	}

	if err := queue.RemoveFirstSong(guildID); err != nil {
		logger.Errorf("Failed to remove finished song: %v", err)
	}

	if !shouldRepeat {
		return
	}

	position := -1
	if repeatMode == queue.RepeatSingle {
		position = 0
	}

	logger.Debugf("Repeating %s at position %d", repeatSong.Title, position)
	if err := queue.AddSong(guildID, repeatSong, position); err != nil {
		logger.Errorf("Failed to re-add song for repeat: %v", err)
	}
}

func reloadNormalization(guildID string, current bool) bool {
	normalization, err := queue.GetNormalization(guildID)
	if err != nil {
		logger.Warnf("Failed to get normalization state, using previous: %v", err)
		return current
	}
	return normalization
}

func reloadVolumeAndFade(player *GuildPlayer, guildID string, fade *fadeSettings) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil {
		return
	}

	player.mu.Lock()
	player.Volume = q.Volume / 100.0
	player.mu.Unlock()
	*fade = fadeSettingsFromQueue(q)
}

func playSingleSong(session *discordgo.Session, guildID string) playResult {

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		logger.Errorf("Failed to get queue: %v", err)
		announceLeaving(session, guildID, "error")
		if stopErr := stopInternal(guildID); stopErr != nil {
			logger.Errorf("Failed to stop player for %s: %v", guildID, stopErr)
		}
		return playStop
	}

	if q == nil || len(q.Songs) == 0 {
		logger.Debugf("Queue is empty for guild: %s", guildID)
		announceLeaving(session, guildID, "empty")

		if err := stopInternal(guildID); err != nil {
			logger.Errorf("Failed to cleanup: %v", err)
		}
		return playStop
	}

	player := GetPlayer(guildID)
	song := q.Songs[0]

	player.mu.Lock()
	player.Volume = float64(q.Volume) / 100.0

	player.StopChan = make(chan struct{})
	player.mu.Unlock()
	logger.Debugf("Set initial volume to %.0f%% (%.2f) for guild: %s", q.Volume, player.Volume, guildID)

	if err := prepareVoiceConnection(session, player, guildID, q.VoiceChannelID); err != nil {
		logger.Errorf("Failed to join voice: %v", err)
		return playStop
	}

	markPlayerLoading(player, guildID)

	logger.Infof("Starting playback: %s", song.Title)

	voiceChannelBitrate := lookupVoiceChannelBitrate(session, q.VoiceChannelID)

	song.SetState(queue.SongStateLoading)

	streamURL, hasPending, streamErr := resolveSongStreamURL(player, song, guildID, q.SponsorBlock, voiceChannelBitrate)
	if streamErr != nil {
		logger.Errorf("Failed to get stream URL: %v", streamErr)

		select {
		case <-player.StopChan:
			logger.Debugf("Stop signal received during stream URL fetch, stopping: %s", song.Title)
			return playStop
		default:
		}

		shouldRetry := handlePlaybackError(session, guildID, song, streamErr)
		if shouldRetry {

			select {
			case <-player.StopChan:
				logger.Debugf("Drained stale stop signal before retry for: %s", song.Title)
				return playStop
			default:
			}
			time.Sleep(2 * time.Second)
			return playContinue
		}

		if err := queue.RemoveFirstSong(guildID); err != nil {
			logger.Errorf("Failed to remove failed song: %v", err)
		}
		clearAnnounced(guildID)
		return playContinue
	}

	qRecheck, err := queue.GetQueue(guildID, false)
	if err != nil || qRecheck == nil || len(qRecheck.Songs) == 0 {
		logger.Debugf("Queue empty after loading, song was likely skipped: %s", song.Title)
		return playStop
	}

	if qRecheck.Songs[0].ID != song.ID {
		logger.Debugf("Song changed while loading (was: %s, now: %s), restarting", song.Title, qRecheck.Songs[0].Title)
		return playContinue
	}

	firstFrameCh := make(chan struct{}, 1)
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	abortPlayback := func() { abortOnce.Do(func() { close(abortCh) }) }
	defer abortPlayback()

	go announceOnFirstFrame(session, player, song, q, guildID, hasPending, firstFrameCh, abortCh)

	seekTime := song.SeekTime
	normalization := q.Normalization
	fade := fadeSettingsFromQueue(q)
	announceNext := func(next *queue.Song) {
		nq, err := queue.GetQueue(guildID, false)
		if err != nil || nq == nil {
			return
		}
		if !markAnnounced(guildID, next.ID) {
			return
		}
		announceNowPlaying(session, guildID, next, nq)
	}
	for {
		logger.Debugf("Calling playAudio for: %s (seekTime: %d, volume: %g, normalization: %v)", song.Title, seekTime, q.Volume, normalization)
		err := playAudio(player, song, streamURL, seekTime, normalization, voiceChannelBitrate, firstFrameCh, fade, announceNext)
		if err == nil {
			break
		}

		player.mu.Lock()
		toggling := player.TogglingNorm
		seeking := player.Seeking
		if toggling {
			player.TogglingNorm = false

			seekTime = int(time.Since(player.PlaybackStart).Milliseconds())
			player.StopChan = make(chan struct{})
		}
		if seeking {
			player.Seeking = false

			seekTime = player.SeekTargetMs
			song.SeekTime = seekTime
			player.FadeInNext = true
			player.StopChan = make(chan struct{})
		}
		player.mu.Unlock()

		restartReason := ""
		if toggling {
			normalization = reloadNormalization(guildID, normalization)
			restartReason = "normalization toggle"
		} else if seeking {
			reloadVolumeAndFade(player, guildID, &fade)
			restartReason = "seek"
		}

		if restartReason != "" {
			restartURL, restartErr := resolveRestartStreamURL(guildID, song, q.SponsorBlock, voiceChannelBitrate, streamURL)
			if restartErr != nil {
				logger.Warnf("Failed to resolve stream URL for %s: %v", restartReason, restartErr)
				return playContinue
			}
			streamURL = restartURL
			logger.Debugf("Restarting FFmpeg for %s at %dms: %s", restartReason, seekTime, song.Title)
			continue
		}

		if err.Error() == "playback stopped by user" {
			logger.Debugf("Playback stopped by user for: %s", song.Title)

			return playStop
		}

		player.mu.Lock()
		currentPosition := int(time.Since(player.PlaybackStart).Milliseconds())
		player.mu.Unlock()

		if currentPosition > song.SeekTime+1000 {
			song.SeekTime = currentPosition
			logger.Infof("Crash recovery: will resume from position %dms for: %s", currentPosition, song.Title)

			if err := queue.UpdateSongSeekTime(guildID, song.ID, currentPosition); err != nil {
				logger.Warnf("Failed to update seek time in database: %v", err)
			}
		}

		isStreamStall := strings.Contains(err.Error(), "stream stalled")
		if isStreamStall {
			announceReconnect(session, guildID, song)
		}

		isVoiceError := strings.Contains(err.Error(), "voice connection")
		if isVoiceError {
			logger.Warnf("Voice connection error detected, clearing dead connection for guild: %s", guildID)
			player.mu.Lock()
			if player.VoiceConn != nil {

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				player.VoiceConn.Disconnect(ctx)
				cancel()
				player.VoiceConn = nil
				player.VoiceChannelID = ""
			}
			player.mu.Unlock()
		}

		logger.Errorf("Playback error: %v", err)
		shouldRetry := handlePlaybackError(session, guildID, song, err)
		if shouldRetry {

			select {
			case <-player.StopChan:
				logger.Debugf("Drained stale stop signal before retry for: %s", song.Title)
				return playStop
			default:
			}

			if isVoiceError {
				logger.Infof("Waiting 3 seconds before reconnecting voice for guild: %s", guildID)
				time.Sleep(3 * time.Second)
			} else {
				invalidatePreCacheSong(guildID, song.ID)
				time.Sleep(2 * time.Second)
			}
			abortPlayback()
			return playContinue
		}

		if err := queue.RemoveFirstSong(guildID); err != nil {
			logger.Errorf("Failed to remove failed song: %v", err)
		}
		clearAnnounced(guildID)
		dismissLoadingMessage(session, guildID)
		abortPlayback()
		return playContinue
	}
	abortPlayback()
	logger.Debugf("playAudio completed successfully for: %s", song.Title)

	player.mu.Lock()
	player.Playing = false
	advanced := player.AutoMixAdvanced
	player.AutoMixAdvanced = false
	player.mu.Unlock()

	if advanced {
		go preCacheNext(guildID, voiceChannelBitrate)
		return playContinue
	}

	song.ResetRetry()
	song.SetState(queue.SongStateCompleted)
	clearRetryCount(guildID, song.URL)
	clearAnnounced(guildID)

	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("Failed to clear playing state after song finish: %v", err)
	}

	advanceQueueAfterSong(guildID, song)

	go preCacheNext(guildID, voiceChannelBitrate)

	return playContinue
}
