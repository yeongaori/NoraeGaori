package player

import (
	"fmt"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

func GetTrimRange(guildID string) (int, int) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()
	return player.TrimStartMs, player.TrimEndMs
}

func fadeSettingsFromQueue(q *queue.Queue) fadeSettings {
	return fadeSettings{
		fadeIn:         q.FadeIn,
		fadeOut:        q.FadeOut,
		autoMix:        q.AutoMix,
		crossfade:      q.Crossfade || q.AutoMix,
		trimSilence:    q.TrimSilence || q.AutoMix,
		fadeInSec:      q.FadeInDuration,
		fadeOutSec:     q.FadeOutDuration,
		crossfadeSec:   q.CrossfadeDuration,
		autoMixBeats:   q.AutoMixBeats,
		repeatMode:     q.RepeatMode,
		styleOverrides: q.AutoMixOverrides,
	}
}

func planFadeOutWindow(totalFrames, sentFrames int, fadeOutSec float64) (int, int) {
	remaining := totalFrames - sentFrames
	frames := int(fadeOutSec * dsp.FramesPerSecond)
	if frames > remaining {
		frames = remaining
	}
	if frames <= 0 {
		return 0, 0
	}
	return totalFrames - frames, frames
}

func advanceQueueForAutoMix(player *GuildPlayer, song *queue.Song, crossfade *crossfadeState, announceNext func(*queue.Song)) {
	guildID := player.GuildID

	song.ResetRetry()
	song.SetState(queue.SongStateCompleted)
	clearRetryCount(guildID, song.URL)

	q, err := queue.GetQueue(guildID, false)
	if err != nil {
		crossfade.scope.Errorf("Failed to load queue for advancement: %v", err)
	}

	repeatMode := queue.RepeatOff
	if q != nil {
		repeatMode = q.RepeatMode
	}
	var repeatSong *queue.Song
	if repeatMode != queue.RepeatOff && !song.IsLive {
		repeatSong = repeatCopyOf(song)
	}

	if err = queue.RemoveFirstSong(guildID); err != nil {
		crossfade.scope.Errorf("Failed to remove finished song: %v", err)
		return
	}

	if repeatSong != nil {
		if err = queue.AddSong(guildID, repeatSong, -1); err != nil {
			crossfade.scope.Errorf("Failed to re-add song for queue repeat: %v", err)
		}
	}

	player.mu.Lock()
	player.AutoMixAdvanced = true
	player.PlaybackStart = time.Now().Add(-time.Duration(crossfade.bSeekSec * float64(time.Second)))
	player.mu.Unlock()

	callOnSongStart(guildID)

	q, err = queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return
	}
	next := q.Songs[0]
	if next.ID != crossfade.nextSongID {
		return
	}
	next.StartPlayback()
	if announceNext != nil {
		go announceNext(next)
	}
	crossfade.scope.Debugf("advanced queue to song ID %d at crossfade start for guild: %s", next.ID, guildID)
}

func opusBitrateFor(channelBitrate int) int {
	bitrate := channelBitrate
	if bitrate < 64000 {
		bitrate = 64000
	}
	if bitrate > 510000 {
		bitrate = 510000
	}
	return bitrate
}

func fadeInGainAt(frame, startFrame, frames int) (float64, bool) {
	if frames <= 0 || frame >= startFrame+frames {
		return 1.0, false
	}
	return dsp.QSinIn(float64(frame-startFrame) / float64(frames)), true
}

func fadeOutGainAt(frame, startFrame, frames int) (float64, bool) {
	if frames <= 0 || frame < startFrame {
		return 1.0, false
	}
	return dsp.QSinOut(float64(frame-startFrame) / float64(frames)), true
}

func openPlaybackStream(player *GuildPlayer, song *queue.Song, streamURL string, seekTime, bitrate int, normalization, collectTail bool) (audioStream, error) {
	if song.IsLive {
		logger.Debugf("Opening live yt-dlp pipe for guild: %s", player.GuildID)
		return player.startLiveStream(song.URL, bitrate, normalization, collectTail)
	}

	if streamURL == "" {
		return nil, fmt.Errorf("no stream URL available for playback")
	}

	logger.Debugf("Building FFmpeg command for guild: %s", player.GuildID)
	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
	}

	if seekTime > 0 {
		seekSeconds := float64(seekTime) / 1000.0
		args = append(args, "-ss", fmt.Sprintf("%.3f", seekSeconds))
	}

	args = append(args, "-i", streamURL)

	if normalization {
		args = append(args, "-af", "dynaudnorm=framelen=500:gausssize=31:peak=0.95")
	}

	args = append(args,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"pipe:1",
	)

	return player.startStream(args, collectTail)
}

func acquireOpusEncoder(resumeMode bool, pending *PendingStream, bitrate int, guildID string) (*opus.Encoder, error) {
	if resumeMode && pending != nil && pending.Encoder != nil {
		logger.Debugf("Reusing Opus encoder from handoff for guild: %s", guildID)
		return pending.Encoder, nil
	}

	logger.Debugf("Creating Opus encoder (%s) for guild: %s", opus.GetEncoderType(), guildID)
	encoder, err := opus.NewEncoder(frameRate, channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus encoder: %w", err)
	}

	opusBitrate := opusBitrateFor(bitrate)
	if err := encoder.SetBitrate(opusBitrate); err != nil {
		logger.Warnf("Failed to set opus bitrate: %v", err)
	} else {
		logger.Debugf("Opus bitrate set to %d bps (channel: %d bps) for guild: %s", opusBitrate, bitrate, guildID)
	}

	return encoder, nil
}

func classifyDrainedStream(stream audioStream, song *queue.Song, sentFrames, frameOffset int, resumeMode bool) error {
	select {
	case err := <-stream.Errs():
		return err
	default:
	}

	if sentFrames == 0 {
		if resumeMode && frameOffset > 0 && stream.EndState() != nil {
			return nil
		}
		return fmt.Errorf("playback completed with no audio frames sent")
	}

	if song.IsLive {
		if active, _ := youtube.IsLiveStreamActive(song.URL); active {
			return fmt.Errorf("stream stalled: live stream interrupted")
		}
	}

	return nil
}

func adjustEndStateForOffset(es *ffmpeg.EndState, frameOffset int) *ffmpeg.EndState {
	if frameOffset <= 0 {
		return es
	}

	return &ffmpeg.EndState{
		TotalFrames:      es.TotalFrames - frameOffset,
		Analysis:         es.Analysis,
		TailStartFrame:   es.TailStartFrame - frameOffset,
		SilentTailFrames: es.SilentTailFrames,
	}
}

func playAudio(player *GuildPlayer, song *queue.Song, streamURL string, seekTime int, normalization bool, bitrate int, firstFrameCh chan<- struct{}, fade *fadeSettings, announceNext func(*queue.Song)) error {
	logger.Debugf("Entered function for guild: %s", player.GuildID)

	stopCh := player.StopChan

	defer signalPlaybackDone(player)
	defer clearFadeFlags(player)

	guildID := markPlaybackStarted(player, song, seekTime)

	retries := notifySongStart(guildID, song)

	collectTail := (fade.autoMix || fade.trimSilence) && !song.IsLive

	stream, resumed := adoptPendingStream(player, song, seekTime, fade)

	if retries == 0 {
		go preCacheNext(guildID, bitrate)
	}

	if stream == nil {
		opened, openErr := openPlaybackStream(player, song, streamURL, seekTime, bitrate, normalization, collectTail)
		if openErr != nil {
			return openErr
		}
		stream = opened
	}

	logger.Debugf("FFmpeg started, setting voice speaking state for guild: %s", guildID)

	logger.Debugf("About to call Speaking(true) for guild: %s", guildID)
	conn := player.currentVoice()
	if conn == nil {
		stream.Stop()
		return fmt.Errorf("voice connection is nil")
	}
	_ = conn.Speaking(true)
	logger.Debugf("Speaking(true) completed for guild: %s", guildID)
	var session *playbackSession
	defer func() {
		player.mu.Lock()
		handingOff := player.PendingStream != nil
		player.mu.Unlock()
		if session != nil {
			conn = session.voice
		}
		if !handingOff {
			_ = conn.Speaking(false)
		}
	}()

	opusEncoder, encoderErr := acquireOpusEncoder(resumed != nil, resumed, bitrate, guildID)
	if encoderErr != nil {
		stream.Stop()
		return encoderErr
	}

	defer player.transitionArmed.Store(false)

	session = buildPlaybackSession(player, song, stopCh, stream, opusEncoder, resumed, seekTime, normalization, bitrate, fade)
	return session.run(firstFrameCh, announceNext)
}

func signalPlaybackDone(player *GuildPlayer) {
	select {
	case player.PlaybackDone <- struct{}{}:
		logger.Debugf("Signaled PlaybackDone for guild: %s", player.GuildID)
	default:

		select {
		case <-player.PlaybackDone:
		default:
		}
		select {
		case player.PlaybackDone <- struct{}{}:
		default:
		}
	}
}

func clearFadeFlags(player *GuildPlayer) {
	player.mu.Lock()
	player.FadingOut = false
	player.FadingIn = false
	player.mu.Unlock()
}

func markPlaybackStarted(player *GuildPlayer, song *queue.Song, seekTime int) string {
	player.mu.Lock()
	player.Playing = true
	player.Loading = false
	player.FadingOut = false
	player.FadingIn = false
	player.TrimStartMs = 0
	player.TrimEndMs = 0
	player.PlaybackStart = time.Now().Add(-time.Duration(seekTime) * time.Millisecond)
	guildID := player.GuildID
	player.mu.Unlock()

	song.StartPlayback()

	if err := queue.SetPlaying(guildID, true); err != nil {
		logger.Errorf("Failed to set playing state: %v", err)
	}
	if err := queue.SetLoading(guildID, false); err != nil {
		logger.Errorf("Failed to set loading state: %v", err)
	}

	logger.Debugf("Set playing state for guild: %s", guildID)
	return guildID
}

func notifySongStart(guildID string, song *queue.Song) int {
	playbackRetriesMu.Lock()
	retries := playbackRetries[retryKey(guildID, song.URL)]
	playbackRetriesMu.Unlock()
	if retries == 0 {
		callOnSongStart(guildID)
		logger.Debugf("Called onSongStart callback for guild: %s", guildID)
	} else {
		logger.Debugf("Skipping onSongStart callback (retry %d) for guild: %s", retries, guildID)
	}
	return retries
}

func adoptPendingStream(player *GuildPlayer, song *queue.Song, seekTime int, fade *fadeSettings) (audioStream, *PendingStream) {
	player.mu.Lock()
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending == nil {
		return nil, nil
	}
	if song.IsLive || pending.SongID != song.ID || seekTime != 0 {
		pending.Stream.Stop()
		return nil, nil
	}

	offset := time.Duration(pending.FramesConsumed)*20*time.Millisecond +
		time.Duration(pending.StartOffsetSec*float64(time.Second))
	player.mu.Lock()
	player.PlaybackStart = time.Now().Add(-offset)
	if fade.trimSilence {
		player.TrimStartMs = int(pending.StartOffsetSec*1000) + pending.LeadingSkipFrames*20
	}
	player.mu.Unlock()
	logger.Debugf("Resuming handed-off stream for guild: %s", player.GuildID)
	return pending.Stream, pending
}

func buildPlaybackSession(player *GuildPlayer, song *queue.Song, stopCh chan struct{}, stream audioStream, opusEncoder *opus.Encoder, resumed *PendingStream, seekTime int, normalization bool, bitrate int, fade *fadeSettings) *playbackSession {
	resumeMode := resumed != nil
	frameOffset := 0
	baseOffsetMs := seekTime
	if resumeMode {
		frameOffset = resumed.FramesConsumed
		baseOffsetMs = int(resumed.StartOffsetSec * 1000)
	}

	player.mu.Lock()
	fadeInNext := player.FadeInNext
	player.FadeInNext = false
	player.mu.Unlock()

	session := &playbackSession{
		player:         player,
		song:           song,
		guildID:        player.GuildID,
		stopCh:         stopCh,
		stream:         stream,
		opusEncoder:    opusEncoder,
		crossfade:      newCrossfadeState(),
		outro:          newOutroState(),
		normalization:  normalization,
		bitrate:        bitrate,
		baseOffsetMs:   baseOffsetMs,
		frameOffset:    frameOffset,
		resumeMode:     resumeMode,
		fade:           *fade,
		volumeBuf:      make([]int16, frameSize*channels),
		opusScratch:    make([]byte, maxOpusFrameBytes),
		skipLeading:    fade.trimSilence && !resumeMode && seekTime == 0,
		heldFadeInGain: 1.0,
		replanAllowed:  true,
	}

	if resumeMode && resumed.Tail != nil {
		session.activeTail = resumed.Tail
		logger.Debugf("Carrying transition tail into song ID %d for guild: %s", song.ID, player.GuildID)
	}

	if fade.fadeIn && !resumeMode && (seekTime == 0 || fadeInNext) {
		session.fadeInFrames = int(fade.fadeInSec * dsp.FramesPerSecond)
	}
	return session
}
