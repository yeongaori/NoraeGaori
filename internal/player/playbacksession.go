package player

import (
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
)

func (s *playbackSession) run(firstFrameCh chan<- struct{}, announceNext func(*queue.Song)) error {
	for {
		conn := s.player.currentVoice()
		s.voice = conn
		if conn == nil {
			s.stream.Stop()
			s.crossfade.abort()
			return fmt.Errorf("voice connection is nil")
		}

		select {
		case pcmData, ok := <-s.stream.PCM():
			if !ok {
				if done, err := s.crossfade.finishOnDrain(s.player, conn, s.stopCh, s.opusEncoder, &s.sentFrames); done {
					return err
				}
				s.outro.flush(s.player, conn, s.stopCh, s.opusEncoder, &s.sentFrames)
				return classifyDrainedStream(s.stream, s.song, s.sentFrames, s.frameOffset, s.resumeMode)
			}

			if done, err := s.playFrame(pcmData, firstFrameCh, announceNext); done {
				return err
			}

		case <-conn.DeadChan():
			s.stream.Stop()
			s.crossfade.abort()
			return fmt.Errorf("voice connection died: %v", conn.Err())

		case <-s.stopCh:
			s.stream.Stop()
			s.crossfade.abort()
			return fmt.Errorf("playback stopped by user")
		}
	}
}

func (s *playbackSession) playFrame(pcmData []int16, firstFrameCh chan<- struct{}, announceNext func(*queue.Song)) (bool, error) {
	if !s.endPlanned {
		s.planEndOfStream()
	} else if s.readyToReplan() {
		s.replanTransition()
	}
	s.recheckPreCache()

	s.publishTransitionArmed()

	if s.consumeLeadingSilence(pcmData) {
		return false, nil
	}

	if s.reachedSilentTail() {
		logger.Debugf("Trimming %d trailing silent frames for guild: %s", s.endStateAdj.SilentTailFrames, s.guildID)
		s.outro.flush(s.player, s.voice, s.stopCh, s.opusEncoder, &s.sentFrames)
		return true, nil
	}

	s.player.mu.Lock()
	volumeFactor := s.player.Volume
	ramping := s.player.Ramping
	s.player.mu.Unlock()

	gain := s.frameGain(ramping)

	wasActive := s.crossfade.active
	if done, err := s.crossfade.consume(s.player, s.voice, s.stopCh, pcmData, volumeFactor, s.opusEncoder, &s.sentFrames); done {
		return true, err
	} else if s.crossfade.active {
		if !wasActive {
			advanceQueueForAutoMix(s.player, s.song, s.crossfade, announceNext)
		}
		return false, nil
	}

	s.replanFadeOutAfterCancel()
	s.shapeFrame(pcmData, volumeFactor, gain)

	if err := s.encodeAndSend(firstFrameCh); err != nil {
		return true, err
	}
	return false, nil
}

type playbackSession struct {
	player      *GuildPlayer
	song        *queue.Song
	guildID     string
	stopCh      chan struct{}
	stream      audioStream
	opusEncoder *opus.Encoder
	crossfade   *crossfadeState
	voice       voiceConnection
	outro       *outroState
	activeTail  *transition.Tail

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
	opusScratch          []byte
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
	endStateAdj          *ffmpeg.EndState
}

func settleTailAnalysis(song *queue.Song, es *ffmpeg.EndState, baseOffsetMs int) *ffmpeg.EndState {
	if es.Analysis != nil {
		es.Analysis.Offset = float64(baseOffsetMs)/1000 + float64(es.TailStartFrame)/dsp.FramesPerSecond
	}
	tail, isLive := chooseTailAnalysis(es.Analysis, analysis.LoadTrackAnalysis(song.URL, analysis.SegmentTail))
	if isLive {
		if saveErr := analysis.SaveTrackAnalysis(song.URL, analysis.SegmentTail, tail); saveErr != nil {
			logger.Warnf("Failed to save tail analysis for %s: %v", song.Title, saveErr)
		}
		return es
	}
	if tail == nil {
		return es
	}
	logger.Debugf("Using the stored tail analysis for %s (%.1fs over %.1fs live)", song.Title, tail.Duration, analysisDuration(es.Analysis))
	chosen := *es
	chosen.Analysis = tail
	return &chosen
}

func (s *playbackSession) planEndOfStream() {
	es := s.stream.EndState()
	if es == nil {
		return
	}

	s.endPlanned = true
	nq, qErr := queue.GetQueue(s.guildID, false)
	if qErr == nil && nq != nil {
		s.fade = fadeSettingsFromQueue(nq)
	}
	s.logNextSongReadiness(nq, es)
	go preCacheNext(s.guildID, s.bitrate)

	if s.song.IsLive {
		return
	}

	es = settleTailAnalysis(s.song, es, s.baseOffsetMs)
	if s.fade.trimSilence && es.SilentTailFrames > 0 {
		s.player.mu.Lock()
		s.player.TrimEndMs = s.baseOffsetMs + (es.TotalFrames-es.SilentTailFrames)*20
		s.player.mu.Unlock()
	}

	es = adjustEndStateForOffset(es, s.frameOffset)
	s.endStateAdj = es

	planned := s.crossfade.plan(s.player, es, s.sentFrames, s.originSec(), &s.fade, s.normalization, s.bitrate)
	if !planned {
		planned = s.outro.plan(s.player, es, s.sentFrames, &s.fade)
	}
	if !planned && s.fade.fadeOut {
		s.fadeOutStartFrame, s.fadeOutFrames = planFadeOutWindow(es.TotalFrames-es.SilentTailFrames, s.sentFrames, s.fade.fadeOutSec)
		logger.Debugf("Fade-out window planned: start frame %d, %d frames (total %d, sent %d) for guild: %s", s.fadeOutStartFrame, s.fadeOutFrames, es.TotalFrames, s.sentFrames, s.guildID)
	}
}

func (s *playbackSession) logNextSongReadiness(q *queue.Queue, es *ffmpeg.EndState) {
	if s.song.IsLive || q == nil || len(q.Songs) < 2 {
		return
	}

	next := q.Songs[1]
	urlState, headState := "live", "live"
	if !next.IsLive {
		urlState = "missing"
		if GetCachedStreamURL(s.guildID, next.ID) != "" {
			urlState = "cached"
		}
		headState = describeHeadAnalysis(s.guildID, next)
	}

	remainingSec := float64(es.TotalFrames-s.frameOffset-es.SilentTailFrames-s.sentFrames) / dsp.FramesPerSecond
	logger.Debugf("End of %s known with %.1fs left; next song %s: stream URL %s, head analysis %s for guild: %s",
		s.song.Title, remainingSec, next.Title, urlState, headState, s.guildID)
}

func (s *playbackSession) originSec() float64 {
	return float64(s.baseOffsetMs)/1000 + float64(s.frameOffset)/dsp.FramesPerSecond
}

func (s *playbackSession) readyToReplan() bool {
	return s.endStateAdj != nil && s.replanAllowed && !s.crossfade.armed && !s.crossfade.cancelled &&
		(s.fade.autoMix || s.fade.crossfade) && !s.outro.committed &&
		!(s.fadeOutFrames > 0 && s.sentFrames >= s.fadeOutStartFrame) &&
		s.sentFrames%50 == 0
}

func (s *playbackSession) replanTransition() {
	go preCacheNext(s.guildID, s.bitrate)

	if s.crossfade.plan(s.player, s.endStateAdj, s.sentFrames, s.originSec(), &s.fade, s.normalization, s.bitrate) {
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

	if !s.outro.armed && s.outro.plan(s.player, s.endStateAdj, s.sentFrames, &s.fade) {
		if s.fadeOutFrames > 0 {
			logger.Debugf("Fade-out window cleared, outro armed for guild: %s", s.guildID)
		}
		s.fadeOutStartFrame = 0
		s.fadeOutFrames = 0
	}
}

func (s *playbackSession) recheckPreCache() {
	if s.endPlanned || s.sentFrames == 0 || s.sentFrames%preCacheIntervalFrames != 0 {
		return
	}
	go preCacheNext(s.guildID, s.bitrate)
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

	if dsp.FrameSilent(pcmData) {
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
		logger.Debugf("Skipped %d leading silent frames (%.1fs) for guild: %s", s.skippedLeadingFrames, float64(s.skippedLeadingFrames)/dsp.FramesPerSecond, s.guildID)
	}
	return false
}

func (s *playbackSession) reachedSilentTail() bool {
	return s.fade.trimSilence && s.endStateAdj != nil && s.endStateAdj.SilentTailFrames > 0 &&
		!s.crossfade.armed && s.sentFrames >= s.endStateAdj.TotalFrames-s.endStateAdj.SilentTailFrames
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

	if es := s.stream.EndState(); es != nil {
		s.fadeOutStartFrame, s.fadeOutFrames = planFadeOutWindow(es.TotalFrames-s.frameOffset-es.SilentTailFrames, s.sentFrames, s.fade.fadeOutSec)
		logger.Debugf("Fade-out window planned after cancel: start frame %d, %d frames for guild: %s", s.fadeOutStartFrame, s.fadeOutFrames, s.guildID)
	}
}

func (s *playbackSession) shapeFrame(pcmData []int16, volumeFactor, gain float64) {
	copy(s.volumeBuf, pcmData)

	if s.outro.running(s.sentFrames) {
		s.outro.process(s.volumeBuf, s.sentFrames, volumeFactor*gain)
	} else {
		dsp.ApplyGain(s.volumeBuf, volumeFactor*gain)
	}

	if s.activeTail != nil && !s.activeTail.Apply(s.volumeBuf) {
		s.activeTail = nil
	}
}

func sendFrame(conn voiceConnection, packet []byte, stopCh <-chan struct{}) error {
	select {
	case <-conn.DeadChan():
		return fmt.Errorf("voice connection died: %v", conn.Err())
	default:
	}

	select {
	case conn.OpusSendChan() <- packet:
		return nil
	case <-conn.DeadChan():
		return fmt.Errorf("voice connection died: %v", conn.Err())
	case <-stopCh:
		return fmt.Errorf("playback stopped by user")
	}
}

func (s *playbackSession) encodeAndSend(firstFrameCh chan<- struct{}) error {
	opusLen, err := s.opusEncoder.Encode(s.volumeBuf, s.opusScratch)
	if err != nil {
		logger.Errorf("Opus encoding error: %v", err)
		s.sentFrames++
		return nil
	}

	packet := make([]byte, opusLen)
	copy(packet, s.opusScratch[:opusLen])

	sendStart := time.Now()
	if err := sendFrame(s.voice, packet, s.stopCh); err != nil {
		s.stream.Stop()
		s.crossfade.abort()
		return err
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
