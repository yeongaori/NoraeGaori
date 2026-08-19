package player

import (
	"fmt"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

func GetTrimRange(guildID string) (int, int) {
	player := GetPlayer(guildID)
	player.mu.Lock()
	defer player.mu.Unlock()
	return player.TrimStartMs, player.TrimEndMs
}

func fadeSettingsFromQueue(q *queue.Queue) fadeSettings {
	return fadeSettings{
		fadeIn:       q.FadeIn,
		fadeOut:      q.FadeOut,
		autoMix:      q.AutoMix,
		crossfade:    q.Crossfade || q.AutoMix,
		trimSilence:  q.TrimSilence || q.AutoMix,
		fadeInSec:    q.FadeInDuration,
		fadeOutSec:   q.FadeOutDuration,
		crossfadeSec: q.CrossfadeDuration,
		autoMixBeats: q.AutoMixBeats,
		repeatMode:   q.RepeatMode,
		styleOverrides: TransitionStyleOverrides{
			Volume: q.AutoMixStyleVolume,
			EQ:     q.AutoMixStyleEQ,
			Filter: q.AutoMixStyleFilter,
			Effect: q.AutoMixStyleEffect,
			Loop:   q.AutoMixStyleLoop,
		},
	}
}

func planFadeOutWindow(totalFrames, sentFrames int, fadeOutSec float64) (int, int) {
	remaining := totalFrames - sentFrames
	frames := int(fadeOutSec * framesPerSecond)
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
		repeatSong = &queue.Song{
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

	if err := queue.RemoveFirstSong(guildID); err != nil {
		crossfade.scope.Errorf("Failed to remove finished song: %v", err)
		return
	}

	if repeatSong != nil {
		if err := queue.AddSong(guildID, repeatSong, -1); err != nil {
			crossfade.scope.Errorf("Failed to re-add song for queue repeat: %v", err)
		}
	}

	player.mu.Lock()
	player.AutoMixAdvanced = true
	player.PlaybackStart = time.Now().Add(-time.Duration(crossfade.startOffsetSec * float64(time.Second)))
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
	return qsinIn(float64(frame-startFrame) / float64(frames)), true
}

func fadeOutGainAt(frame, startFrame, frames int) (float64, bool) {
	if frames <= 0 || frame < startFrame {
		return 1.0, false
	}
	return qsinOut(float64(frame-startFrame) / float64(frames)), true
}

func openPlaybackStream(song *queue.Song, streamURL string, seekTime, bitrate int, normalization, collectTail bool, guildID string) (*audioStream, error) {
	if song.IsLive {
		logger.Debugf("Opening live yt-dlp pipe for guild: %s", guildID)
		sp, pipeErr := getLiveStreamPipe(song.URL, false, bitrate, 0)
		if pipeErr != nil {
			return nil, pipeErr
		}

		return newAudioStreamPipe(buildFFmpegArgsPipe(normalization), sp, collectTail)
	}

	if streamURL == "" {
		return nil, fmt.Errorf("no stream URL available for playback")
	}

	logger.Debugf("Building FFmpeg command for guild: %s", guildID)
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

	return newAudioStream(args, collectTail)
}

func acquireOpusEncoder(resumeMode bool, pending *PendingStream, bitrate int, guildID string) (*OpusEncoder, error) {
	if resumeMode && pending != nil && pending.Encoder != nil {
		logger.Debugf("Reusing Opus encoder from handoff for guild: %s", guildID)
		return pending.Encoder, nil
	}

	logger.Debugf("Creating Opus encoder (%s) for guild: %s", GetEncoderType(), guildID)
	encoder, err := NewOpusEncoder(frameRate, channels)
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

func classifyDrainedStream(stream *audioStream, song *queue.Song, sentFrames, frameOffset int, resumeMode bool) error {
	select {
	case err := <-stream.errChan:
		return err
	default:
	}

	if sentFrames == 0 {
		if resumeMode && frameOffset > 0 && stream.endState.Load() != nil {
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

func adjustEndStateForOffset(es *streamEndState, frameOffset int) *streamEndState {
	if frameOffset <= 0 {
		return es
	}

	return &streamEndState{
		totalFrames:      es.totalFrames - frameOffset,
		analysis:         es.analysis,
		tailStartFrame:   es.tailStartFrame - frameOffset,
		silentTailFrames: es.silentTailFrames,
	}
}

func playAudio(player *GuildPlayer, song *queue.Song, streamURL string, seekTime int, normalization bool, bitrate int, firstFrameCh chan<- struct{}, fade fadeSettings, announceNext func(*queue.Song)) error {
	logger.Debugf("Entered function for guild: %s", player.GuildID)

	stopCh := player.StopChan

	defer func() {
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
	}()

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

	defer func() {
		player.mu.Lock()
		player.FadingOut = false
		player.FadingIn = false
		player.mu.Unlock()
	}()

	song.StartPlayback()

	if err := queue.SetPlaying(guildID, true); err != nil {
		logger.Errorf("Failed to set playing state: %v", err)
	}
	if err := queue.SetLoading(guildID, false); err != nil {
		logger.Errorf("Failed to set loading state: %v", err)
	}

	logger.Debugf("Set playing state for guild: %s", guildID)

	playbackRetriesMu.Lock()
	retries := playbackRetries[retryKey(guildID, song.URL)]
	playbackRetriesMu.Unlock()
	if retries == 0 {
		callOnSongStart(guildID)
		logger.Debugf("Called onSongStart callback for guild: %s", guildID)
	} else {
		logger.Debugf("Skipping onSongStart callback (retry %d) for guild: %s", retries, guildID)
	}

	collectTail := (fade.autoMix || fade.trimSilence) && !song.IsLive

	var stream *audioStream
	resumeMode := false
	frameOffset := 0

	player.mu.Lock()
	pending := player.PendingStream
	player.PendingStream = nil
	player.mu.Unlock()

	if pending != nil {
		if !song.IsLive && pending.SongID == song.ID && seekTime == 0 {
			stream = pending.Stream
			resumeMode = true
			frameOffset = pending.FramesConsumed
			offset := time.Duration(pending.FramesConsumed)*20*time.Millisecond +
				time.Duration(pending.StartOffsetSec*float64(time.Second))
			player.mu.Lock()
			player.PlaybackStart = time.Now().Add(-offset)
			if fade.trimSilence {
				player.TrimStartMs = int(pending.StartOffsetSec*1000) + pending.LeadingSkipFrames*20
			}
			player.mu.Unlock()
			logger.Debugf("Resuming handed-off stream for guild: %s", guildID)
		} else {
			pending.Stream.stop()
		}
	}

	if retries == 0 {
		go preCacheNext(guildID, bitrate)
	}

	baseOffsetMs := seekTime
	if resumeMode {
		baseOffsetMs = int(pending.StartOffsetSec * 1000)
	}

	if stream == nil {
		opened, openErr := openPlaybackStream(song, streamURL, seekTime, bitrate, normalization, collectTail, guildID)
		if openErr != nil {
			return openErr
		}
		stream = opened
	}

	logger.Debugf("FFmpeg started, setting voice speaking state for guild: %s", guildID)

	logger.Debugf("About to call Speaking(true) for guild: %s", guildID)
	if player.VoiceConn == nil {
		stream.stop()
		return fmt.Errorf("voice connection is nil")
	}
	player.VoiceConn.Speaking(true)
	logger.Debugf("Speaking(true) completed for guild: %s", guildID)
	defer func() {
		player.mu.Lock()
		handingOff := player.PendingStream != nil
		player.mu.Unlock()
		if !handingOff && player.VoiceConn != nil {
			player.VoiceConn.Speaking(false)
		}
	}()

	opusEncoder, encoderErr := acquireOpusEncoder(resumeMode, pending, bitrate, guildID)
	if encoderErr != nil {
		stream.stop()
		return encoderErr
	}

	defer player.transitionArmed.Store(false)

	player.mu.Lock()
	fadeInNext := player.FadeInNext
	player.FadeInNext = false
	player.mu.Unlock()

	session := &playbackSession{
		player:         player,
		song:           song,
		guildID:        guildID,
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
		fade:           fade,
		volumeBuf:      make([]int16, frameSize*channels),
		skipLeading:    fade.trimSilence && !resumeMode && seekTime == 0,
		heldFadeInGain: 1.0,
		replanAllowed:  true,
	}

	if resumeMode && pending.Tail != nil {
		session.activeTail = pending.Tail
		logger.Debugf("Carrying transition tail into song ID %d for guild: %s", song.ID, guildID)
	}

	if fade.fadeIn && !resumeMode && (seekTime == 0 || fadeInNext) {
		session.fadeInFrames = int(fade.fadeInSec * framesPerSecond)
	}

	for {
		select {
		case pcmData, ok := <-stream.pcmChan:
			if !ok {
				if done, err := session.crossfade.finishOnDrain(player, stopCh, opusEncoder, &session.sentFrames); done {
					return err
				}
				session.outro.flush(player, stopCh, opusEncoder, &session.sentFrames)
				return classifyDrainedStream(stream, song, session.sentFrames, frameOffset, resumeMode)
			}

			if !session.endPlanned {
				session.planEndOfStream()
			} else if session.readyToReplan() {
				session.replanTransition()
			}

			session.publishTransitionArmed()

			if session.consumeLeadingSilence(pcmData) {
				continue
			}

			if session.reachedSilentTail() {
				logger.Debugf("Trimming %d trailing silent frames for guild: %s", session.endStateAdj.silentTailFrames, guildID)
				session.outro.flush(player, stopCh, opusEncoder, &session.sentFrames)
				return nil
			}

			player.mu.Lock()
			volumeFactor := player.Volume
			ramping := player.Ramping
			player.mu.Unlock()

			gain := session.frameGain(ramping)

			wasActive := session.crossfade.active
			if done, err := session.crossfade.consume(player, stopCh, pcmData, volumeFactor, opusEncoder, &session.sentFrames); done {
				return err
			} else if session.crossfade.active {
				if !wasActive {
					advanceQueueForAutoMix(player, song, session.crossfade, announceNext)
				}
				continue
			}

			session.replanFadeOutAfterCancel()
			session.shapeFrame(pcmData, volumeFactor, gain)

			if err := session.encodeAndSend(firstFrameCh); err != nil {
				return err
			}

		case <-player.VoiceConn.DeadChan():
			stream.stop()
			session.crossfade.abort()
			return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err())

		case <-stopCh:
			stream.stop()
			session.crossfade.abort()
			return fmt.Errorf("playback stopped by user")
		}
	}
}

type playbackSession struct {
	player      *GuildPlayer
	song        *queue.Song
	guildID     string
	stopCh      chan struct{}
	stream      *audioStream
	opusEncoder *OpusEncoder
	crossfade   *crossfadeState
	outro       *outroState
	activeTail  *transitionTail

	normalization bool
	bitrate       int
	baseOffsetMs  int
	frameOffset   int
	resumeMode    bool
	fade          fadeSettings

	sentFrames           int
	firstFrameSignaled   bool
	retryCreditCleared   bool
	transitionArmedLast  bool
	volumeBuf            []int16
	fadeInFrames         int
	fadeInStartFrame     int
	skipLeading          bool
	skippedLeadingFrames int
	endPlanned           bool
	fadeOutStartFrame    int
	fadeOutFrames        int
	fadingOutSet         bool
	fadingInSet          bool
	heldFadeInGain       float64
	replanAllowed        bool
	endStateAdj          *streamEndState
}

func (s *playbackSession) planEndOfStream() {
	es := s.stream.endState.Load()
	if es == nil {
		return
	}

	s.endPlanned = true
	go preCacheNext(s.guildID, s.bitrate)
	if nq, qErr := queue.GetQueue(s.guildID, false); qErr == nil && nq != nil {
		s.fade = fadeSettingsFromQueue(nq)
	}

	if s.song.IsLive {
		return
	}

	if es.analysis != nil {
		if saveErr := SaveTrackAnalysis(s.song.URL, AnalysisSegmentTail, es.analysis); saveErr != nil {
			logger.Warnf("Failed to save tail analysis for %s: %v", s.song.Title, saveErr)
		}
	}
	if s.fade.trimSilence && es.silentTailFrames > 0 {
		s.player.mu.Lock()
		s.player.TrimEndMs = s.baseOffsetMs + (es.totalFrames-es.silentTailFrames)*20
		s.player.mu.Unlock()
	}

	es = adjustEndStateForOffset(es, s.frameOffset)
	s.endStateAdj = es

	planned := s.crossfade.plan(s.player, es, s.sentFrames, s.fade, s.normalization, s.bitrate)
	if !planned {
		planned = s.outro.plan(s.player, es, s.sentFrames, s.fade)
	}
	if !planned && s.fade.fadeOut {
		s.fadeOutStartFrame, s.fadeOutFrames = planFadeOutWindow(es.totalFrames-es.silentTailFrames, s.sentFrames, s.fade.fadeOutSec)
		logger.Debugf("Fade-out window planned: start frame %d, %d frames (total %d, sent %d) for guild: %s", s.fadeOutStartFrame, s.fadeOutFrames, es.totalFrames, s.sentFrames, s.guildID)
	}
}

func (s *playbackSession) readyToReplan() bool {
	return s.endStateAdj != nil && s.replanAllowed && !s.crossfade.armed && !s.crossfade.cancelled &&
		(s.fade.autoMix || s.fade.crossfade) && !s.outro.committed &&
		!(s.fadeOutFrames > 0 && s.sentFrames >= s.fadeOutStartFrame) &&
		s.sentFrames%50 == 0
}

func (s *playbackSession) replanTransition() {
	go preCacheNext(s.guildID, s.bitrate)

	if s.crossfade.plan(s.player, s.endStateAdj, s.sentFrames, s.fade, s.normalization, s.bitrate) {
		if s.fadeOutFrames > 0 {
			logger.Debugf("Fade-out window cleared, crossfade armed for guild: %s", s.guildID)
		}
		if s.outro.armed {
			logger.Debugf("Outro cancelled, crossfade armed for guild: %s", s.guildID)
			s.outro.cancel()
		}
		s.fadeOutStartFrame = 0
		s.fadeOutFrames = 0
		return
	}

	if !s.outro.armed && s.outro.plan(s.player, s.endStateAdj, s.sentFrames, s.fade) {
		if s.fadeOutFrames > 0 {
			logger.Debugf("Fade-out window cleared, outro armed for guild: %s", s.guildID)
		}
		s.fadeOutStartFrame = 0
		s.fadeOutFrames = 0
	}
}

func (s *playbackSession) publishTransitionArmed() {
	armed := s.crossfade.armed || s.outro.armed
	if armed == s.transitionArmedLast {
		return
	}

	s.transitionArmedLast = armed
	s.player.transitionArmed.Store(armed)
}

func (s *playbackSession) consumeLeadingSilence(pcmData []int16) bool {
	if !s.skipLeading {
		return false
	}

	if frameSilent(pcmData) {
		s.sentFrames++
		s.skippedLeadingFrames++
		return true
	}

	s.skipLeading = false
	if s.skippedLeadingFrames > 0 {
		s.player.mu.Lock()
		s.player.PlaybackStart = s.player.PlaybackStart.Add(-time.Duration(s.skippedLeadingFrames) * 20 * time.Millisecond)
		s.player.TrimStartMs = s.skippedLeadingFrames * 20
		s.player.mu.Unlock()
		s.fadeInStartFrame = s.sentFrames
		logger.Debugf("Skipped %d leading silent frames (%.1fs) for guild: %s", s.skippedLeadingFrames, float64(s.skippedLeadingFrames)/framesPerSecond, s.guildID)
	}
	return false
}

func (s *playbackSession) reachedSilentTail() bool {
	return s.fade.trimSilence && s.endStateAdj != nil && s.endStateAdj.silentTailFrames > 0 &&
		!s.crossfade.armed && s.sentFrames >= s.endStateAdj.totalFrames-s.endStateAdj.silentTailFrames
}

func (s *playbackSession) frameGain(ramping bool) float64 {
	gain := 1.0

	fadeInGain, fadingIn := fadeInGainAt(s.sentFrames, s.fadeInStartFrame, s.fadeInFrames)
	if fadingIn {
		if !ramping {
			s.heldFadeInGain = fadeInGain
		}
		gain *= s.heldFadeInGain
	}
	if fadingIn != s.fadingInSet {
		s.fadingInSet = fadingIn
		s.player.mu.Lock()
		s.player.FadingIn = fadingIn
		s.player.mu.Unlock()
	}

	if fadeOutGain, fadingOut := fadeOutGainAt(s.sentFrames, s.fadeOutStartFrame, s.fadeOutFrames); fadingOut {
		gain *= fadeOutGain
		if !s.fadingOutSet {
			s.fadingOutSet = true
			s.player.mu.Lock()
			s.player.FadingOut = true
			s.player.mu.Unlock()
			logger.Debugf("Fade-out started at frame %d for guild: %s", s.sentFrames, s.guildID)
		}
	}

	return gain
}

func (s *playbackSession) replanFadeOutAfterCancel() {
	if !s.crossfade.cancelled || s.fadeOutFrames != 0 {
		return
	}

	s.crossfade.cancelled = false
	s.replanAllowed = false
	if !s.fade.fadeOut {
		return
	}

	if es := s.stream.endState.Load(); es != nil {
		s.fadeOutStartFrame, s.fadeOutFrames = planFadeOutWindow(es.totalFrames-s.frameOffset-es.silentTailFrames, s.sentFrames, s.fade.fadeOutSec)
		logger.Debugf("Fade-out window planned after cancel: start frame %d, %d frames for guild: %s", s.fadeOutStartFrame, s.fadeOutFrames, s.guildID)
	}
}

func (s *playbackSession) shapeFrame(pcmData []int16, volumeFactor, gain float64) {
	copy(s.volumeBuf, pcmData)

	if s.outro.running(s.sentFrames) {
		s.outro.process(s.volumeBuf, s.sentFrames, volumeFactor*gain)
	} else {
		applyGain(s.volumeBuf, volumeFactor*gain)
	}

	if s.activeTail != nil && !s.activeTail.apply(s.volumeBuf) {
		s.activeTail = nil
	}
}

func (s *playbackSession) encodeAndSend(firstFrameCh chan<- struct{}) error {
	opusBuffer := make([]byte, 1500)
	opusLen, err := s.opusEncoder.Encode(s.volumeBuf, opusBuffer)
	if err != nil {
		logger.Errorf("Opus encoding error: %v", err)
		s.sentFrames++
		return nil
	}

	sendStart := time.Now()
	select {
	case s.player.VoiceConn.OpusSendChan() <- opusBuffer[:opusLen]:
	case <-s.player.VoiceConn.DeadChan():
		s.stream.stop()
		s.crossfade.abort()
		return fmt.Errorf("voice connection died: %v", s.player.VoiceConn.Err())
	case <-s.stopCh:
		s.stream.stop()
		s.crossfade.abort()
		return fmt.Errorf("playback stopped by user")
	}

	if blocked := time.Since(sendStart); blocked > framePacingWarnDelay {
		logger.Warnf("Opus frame delayed %.0fms at frame %d (analysis slots busy: %d) for guild: %s",
			float64(blocked.Microseconds())/1000, s.sentFrames, len(analysisSlots), s.guildID)
	}

	s.sentFrames++
	if !s.firstFrameSignaled {
		s.firstFrameSignaled = true
		logger.Debugf("First Opus frame sent successfully for guild: %s", s.guildID)
		select {
		case firstFrameCh <- struct{}{}:
		default:
		}
	}
	if !s.retryCreditCleared && s.sentFrames >= healthyPlaybackFrames {
		s.retryCreditCleared = true
		clearRetryCount(s.guildID, s.song.URL)
	}

	return nil
}
