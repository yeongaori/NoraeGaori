package player

import (
	"errors"
	"strings"
	"sync"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"

	"github.com/bwmarrin/discordgo"
)

type playResult int

const (
	playContinue playResult = iota
	playStop
)

func prepareVoiceConnection(session *discordgo.Session, player *GuildPlayer, guildID, voiceChannelID string) error {
	conn := player.currentVoice()
	if conn != nil {
		select {
		case <-conn.DeadChan():
			logger.Warnf("Detected dead voice connection, will reconnect for guild: %s", guildID)
			_ = disconnectVoice(conn, 2*time.Second)
			player.setVoice(nil, "")
			conn = nil
		default:
		}
	}

	if conn != nil && !player.isInOtherVoiceChannel(voiceChannelID) {
		return nil
	}

	vc, err := joinVoiceWithGatewayRetry(session, player, voiceChannelID)
	if err != nil {
		return err
	}

	if !player.adoptVoice(vc, voiceChannelID) {
		logger.Debugf("Session halted while joining voice, dropping the new connection for guild: %s", guildID)
		_ = disconnectVoice(vc, 5*time.Second)
		return errSessionHalted
	}

	logger.Debugf("Voice connection established for guild: %s", guildID)
	return nil
}

func (player *GuildPlayer) isInOtherVoiceChannel(channelID string) bool {
	player.mu.Lock()
	defer player.mu.Unlock()
	return channelID != "" && player.VoiceChannelID != "" && player.VoiceChannelID != channelID
}

func (player *GuildPlayer) adoptVoice(conn voiceConnection, channelID string) bool {
	player.mu.Lock()
	defer player.mu.Unlock()

	if player.halted {
		if player.VoiceConn == conn {
			player.VoiceConn = nil
			player.VoiceChannelID = ""
		}
		return false
	}

	player.VoiceConn = conn
	player.VoiceChannelID = channelID
	return true
}

func joinVoiceWithGatewayRetry(session *discordgo.Session, player *GuildPlayer, channelID string) (voiceConnection, error) {
	guildID := player.GuildID
	var conn voiceConnection
	var err error

	for attempt := range voiceRejoinAttempts {
		if player.isHalted() {
			return nil, errSessionHalted
		}

		conn, err = joinVoiceChannel(session, guildID, channelID)
		if err == nil || !errors.Is(err, discordgo.ErrWSNotFound) {
			return conn, err
		}

		if attempt == voiceRejoinAttempts-1 {
			break
		}

		logger.Warnf("Gateway is offline, retrying the voice join (%d/%d) for guild: %s", attempt+1, voiceRejoinAttempts, guildID)
		time.Sleep(voiceRejoinDelay)
	}

	return conn, err
}

func markPlayerLoading(player *GuildPlayer, guildID string) bool {
	player.mu.Lock()
	if player.halted {
		player.mu.Unlock()
		return false
	}
	player.Loading = true
	player.Playing = false
	player.Paused = false
	player.AutoMixAdvanced = false
	player.mu.Unlock()
	forgetAwaitingDiscord(guildID)

	if err := queue.SetPaused(guildID, false); err != nil {
		logger.Errorf("Failed to clear paused state: %v", err)
	}
	if err := queue.SetLoading(guildID, true); err != nil {
		logger.Errorf("Failed to set loading state: %v", err)
	}
	if err := queue.SetPlaying(guildID, false); err != nil {
		logger.Errorf("Failed to set playing state: %v", err)
	}
	return true
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
		URL:              song.URL,
		Title:            song.Title,
		Duration:         song.Duration,
		Thumbnail:        song.Thumbnail,
		Uploader:         song.Uploader,
		RequestedByID:    song.RequestedByID,
		RequestedByTag:   song.RequestedByTag,
		IsLive:           song.IsLive,
		AutoMixOverrides: song.AutoMixOverrides,
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

func endPlayback(session *discordgo.Session, guildID, reason string) error {
	isLeaving := ShouldLeaveVoice(guildID)
	announcePlaybackEnd(session, guildID, reason, isLeaving)
	return stopInternal(guildID, isLeaving)
}

type songPlayback struct {
	session       *discordgo.Session
	player        *GuildPlayer
	song          *queue.Song
	queue         *queue.Queue
	guildID       string
	bitrate       int
	streamURL     string
	hasPending    bool
	seekTime      int
	normalization bool
	fade          fadeSettings
	abort         func()
}

func playSingleSong(session *discordgo.Session, guildID string) playResult {
	p := songPlayback{session: session, guildID: guildID}
	if result, ok := p.loadCurrentSong(); !ok {
		return result
	}
	if !p.claimPlayer() || !p.joinVoiceForSong() {
		return playStop
	}

	logger.Infof("Starting playback: %s", p.song.Title)

	p.bitrate = lookupVoiceChannelBitrate(session, p.queue.VoiceChannelID)

	p.song.SetState(queue.SongStateLoading)

	streamURL, hasPending, streamErr := resolveSongStreamURL(p.player, p.song, guildID, p.queue.SponsorBlock, p.bitrate)
	if streamErr != nil {
		return p.handleStreamURLError(streamErr)
	}
	p.streamURL = streamURL
	p.hasPending = hasPending

	if isCurrent, result := p.isStillCurrentSong(); !isCurrent {
		return result
	}

	firstFrameCh := make(chan struct{}, 1)
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	p.abort = func() { abortOnce.Do(func() { close(abortCh) }) }
	defer p.abort()

	go announceOnFirstFrame(session, p.player, p.song, p.queue, guildID, p.hasPending, firstFrameCh, abortCh)

	if result, isDone := p.playUntilDone(firstFrameCh); !isDone {
		return result
	}
	p.abort()
	logger.Debugf("playAudio completed successfully for: %s", p.song.Title)

	return p.finishSong()
}

func (p *songPlayback) loadCurrentSong() (playResult, bool) {
	q, err := queue.GetQueue(p.guildID, true)
	if err != nil {
		logger.Errorf("Failed to get queue: %v", err)
		if stopErr := endPlayback(p.session, p.guildID, "error"); stopErr != nil {
			logger.Errorf("Failed to stop player for %s: %v", p.guildID, stopErr)
		}
		return playStop, false
	}

	if q == nil || len(q.Songs) == 0 {
		logger.Debugf("Queue is empty for guild: %s", p.guildID)
		if err := endPlayback(p.session, p.guildID, "empty"); err != nil {
			logger.Errorf("Failed to cleanup: %v", err)
		}
		return playStop, false
	}

	p.queue = q
	p.player = GetPlayer(p.guildID)
	p.song = q.Songs[0]
	return playContinue, true
}

func (p *songPlayback) claimPlayer() bool {
	player := p.player
	player.mu.Lock()
	if player.halted {
		player.mu.Unlock()
		return false
	}
	player.Volume = float64(p.queue.Volume) / 100.0

	player.StopChan = make(chan struct{})
	player.mu.Unlock()
	logger.Debugf("Set initial volume to %.0f%% (%.2f) for guild: %s", p.queue.Volume, player.Volume, p.guildID)
	return true
}

func (p *songPlayback) joinVoiceForSong() bool {
	if err := prepareVoiceConnection(p.session, p.player, p.guildID, p.queue.VoiceChannelID); err != nil {
		if errors.Is(err, errSessionHalted) {
			return false
		}
		if isDiscordUnreachable(err) {
			logger.Warnf("Voice is unreachable, playback resumes once Discord reconnects: %v", err)
			markAwaitingDiscord(p.guildID)
			return false
		}
		logger.Errorf("Failed to join voice: %v", err)
		return false
	}

	return markPlayerLoading(p.player, p.guildID)
}

func (p *songPlayback) handleStreamURLError(streamErr error) playResult {
	logger.Errorf("Failed to get stream URL: %v", streamErr)

	select {
	case <-p.player.StopChan:
		logger.Debugf("Stop signal received during stream URL fetch, stopping: %s", p.song.Title)
		return playStop
	default:
	}

	shouldRetry := handlePlaybackError(p.session, p.guildID, p.song, streamErr)
	if shouldRetry {
		if hasStaleStop(p.player, p.song) {
			return playStop
		}
		if !waitBeforeRetry(p.player, streamErr, retryDelay) {
			logger.Debugf("Stop signal received while waiting to retry: %s", p.song.Title)
			return playStop
		}
		return playContinue
	}

	dropFailedSong(p.guildID)
	return playContinue
}

func (p *songPlayback) isStillCurrentSong() (bool, playResult) {
	if p.player.isHalted() {
		logger.Debugf("Session halted while loading, stopping: %s", p.song.Title)
		return false, playStop
	}

	qRecheck, err := queue.GetQueue(p.guildID, false)
	if err != nil || qRecheck == nil || len(qRecheck.Songs) == 0 {
		logger.Debugf("Queue empty after loading, song was likely skipped: %s", p.song.Title)
		return false, playStop
	}

	if qRecheck.Songs[0].ID != p.song.ID {
		logger.Debugf("Song changed while loading (was: %s, now: %s), restarting", p.song.Title, qRecheck.Songs[0].Title)
		return false, playContinue
	}
	return true, playContinue
}

func (p *songPlayback) playUntilDone(firstFrameCh chan struct{}) (playResult, bool) {
	p.seekTime = p.song.SeekTime
	p.normalization = p.queue.Normalization
	p.fade = fadeSettingsFromQueue(p.queue)
	session, guildID := p.session, p.guildID
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
		logger.Debugf("Calling playAudio for: %s (seekTime: %d, volume: %g, normalization: %v)", p.song.Title, p.seekTime, p.queue.Volume, p.normalization)
		err := playAudio(p.player, p.song, p.streamURL, p.seekTime, p.normalization, p.bitrate, firstFrameCh, &p.fade, announceNext)
		if err == nil {
			return playContinue, true
		}

		restartReason, isHalted := p.takeRestartRequest()
		if isHalted {
			return playStop, false
		}

		if restartReason != "" {
			if !p.restart(restartReason) {
				return playContinue, false
			}
			continue
		}

		if err.Error() == "playback stopped by user" {
			logger.Debugf("Playback stopped by user for: %s", p.song.Title)

			return playStop, false
		}

		p.saveCrashPosition()
		return p.handlePlaybackFailure(err), false
	}
}

func (p *songPlayback) takeRestartRequest() (string, bool) {
	player := p.player
	player.mu.Lock()
	if player.halted {
		player.TogglingNorm = false
		player.Seeking = false
		player.mu.Unlock()
		logger.Debugf("Session halted during playback of: %s", p.song.Title)
		return "", true
	}
	toggling := player.TogglingNorm
	seeking := player.Seeking
	if toggling {
		player.TogglingNorm = false

		p.seekTime = int(time.Since(player.PlaybackStart).Milliseconds())
		player.StopChan = make(chan struct{})
	}
	if seeking {
		player.Seeking = false

		p.seekTime = player.SeekTargetMs
		p.song.SeekTime = p.seekTime
		player.FadeInNext = true
		player.StopChan = make(chan struct{})
	}
	player.mu.Unlock()

	restartReason := ""
	if toggling {
		p.normalization = reloadNormalization(p.guildID, p.normalization)
		restartReason = "normalization toggle"
	} else if seeking {
		reloadVolumeAndFade(player, p.guildID, &p.fade)
		restartReason = "seek"
	}
	return restartReason, false
}

func (p *songPlayback) restart(restartReason string) bool {
	restartURL, restartErr := resolveRestartStreamURL(p.guildID, p.song, p.queue.SponsorBlock, p.bitrate, p.streamURL)
	if restartErr != nil {
		logger.Warnf("Failed to resolve stream URL for %s: %v", restartReason, restartErr)
		return false
	}
	p.streamURL = restartURL
	logger.Debugf("Restarting FFmpeg for %s at %dms: %s", restartReason, p.seekTime, p.song.Title)
	return true
}

func (p *songPlayback) saveCrashPosition() {
	p.player.mu.Lock()
	currentPosition := int(time.Since(p.player.PlaybackStart).Milliseconds())
	p.player.mu.Unlock()

	if currentPosition > p.song.SeekTime+1000 {
		p.song.SeekTime = currentPosition
		logger.Infof("Crash recovery: will resume from position %dms for: %s", currentPosition, p.song.Title)

		if err := queue.UpdateSongSeekTime(p.guildID, p.song.ID, currentPosition); err != nil {
			logger.Warnf("Failed to update seek time in database: %v", err)
		}
	}
}

func (p *songPlayback) handlePlaybackFailure(err error) playResult {
	isStreamStall := strings.Contains(err.Error(), "stream stalled")
	if isStreamStall {
		announceReconnect(p.session, p.guildID, p.song)
	}

	isVoiceError := isVoiceConnectionError(err.Error())
	if isVoiceError {
		logger.Warnf("Voice connection error detected, clearing dead connection for guild: %s", p.guildID)
		if conn := p.player.currentVoice(); conn != nil {
			_ = disconnectVoice(conn, 2*time.Second)
			p.player.setVoice(nil, "")
		}
	}

	logger.Errorf("Playback error: %v", err)
	shouldRetry := handlePlaybackError(p.session, p.guildID, p.song, err)
	if shouldRetry {
		if hasStaleStop(p.player, p.song) {
			return playStop
		}

		delay := retryDelay
		if isVoiceError {
			logger.Infof("Waiting 3 seconds before reconnecting voice for guild: %s", p.guildID)
			delay = 3 * time.Second
		} else {
			invalidatePreCacheSong(p.guildID, p.song.ID)
		}
		isRetrying := waitBeforeRetry(p.player, err, delay)
		p.abort()
		if !isRetrying {
			logger.Debugf("Stop signal received while waiting to retry: %s", p.song.Title)
			return playStop
		}
		return playContinue
	}

	dropFailedSong(p.guildID)
	dismissLoadingMessage(p.session, p.guildID)
	p.abort()
	return playContinue
}

func (p *songPlayback) finishSong() playResult {
	player := p.player
	player.mu.Lock()
	player.Playing = false
	advanced := player.AutoMixAdvanced
	player.AutoMixAdvanced = false
	player.mu.Unlock()

	if advanced {
		go preCacheNext(p.guildID, p.bitrate)
		return playContinue
	}

	p.song.ResetRetry()
	p.song.SetState(queue.SongStateCompleted)
	clearRetryCount(p.guildID, p.song.URL)
	clearAnnounced(p.guildID)

	if err := queue.SetPlaying(p.guildID, false); err != nil {
		logger.Errorf("Failed to clear playing state after song finish: %v", err)
	}

	advanceQueueAfterSong(p.guildID, p.song)

	go preCacheNext(p.guildID, p.bitrate)

	return playContinue
}
