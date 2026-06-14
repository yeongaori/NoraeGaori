package player

import (
	"fmt"
	"math"

	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

const (
	tailMarginSec            = 4.0
	crossfadeMinSec          = 3.0
	crossfadeMaxSec          = 20.0
	fallbackCrossfadeSec     = 8.0
	minUsableCrossfadeFrames = 100
	fallbackSlideFrames      = 25
)

type PendingStream struct {
	SongID            int
	Stream            *audioStream
	Encoder           *OpusEncoder
	FramesConsumed    int
	LeadingSkipFrames int
	StartOffsetSec    float64
}

type crossfadeState struct {
	armed           bool
	active          bool
	handedOff       bool
	cancelled       bool
	trimSilence     bool
	bLoudSeen       bool
	fadeGains       bool
	tag             string
	bStream         *audioStream
	nextSongID      int
	startOffsetSec  float64
	transitionFrame int
	crossfadeFrames int
	minUsableFrames int
	totalFrames     int
	slideFrames     int
	mixedFrames     int
	bFramesConsumed int
	bLeadSkipFrames int
	mixBuf          []int16
}

func newCrossfadeState() *crossfadeState {
	return &crossfadeState{tag: "AutoMix", mixBuf: make([]int16, frameSize*channels)}
}

func snapTransitionToGrid(target, tailStartFrame int, a *TrackAnalysis) int {
	if a.PeriodSec <= 0 {
		return target
	}
	periodFrames := a.PeriodSec * framesPerSecond
	firstBeatFrame := float64(tailStartFrame) + a.FirstBeat*framesPerSecond
	k := math.Round((float64(target) - firstBeatFrame) / periodFrames)
	return int(math.Round(firstBeatFrame + k*periodFrames))
}

func (cs *crossfadeState) plan(player *GuildPlayer, es *streamEndState, sentFrames int, fade fadeSettings, normalization bool) bool {
	if (!fade.autoMix && !fade.crossfade) || cs.armed {
		return false
	}
	if fade.repeatMode == queue.RepeatSingle {
		return false
	}

	guildID := player.GuildID
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) < 2 {
		return false
	}
	next := q.Songs[1]
	if next.IsLive {
		return false
	}

	nextURL := GetCachedStreamURL(guildID, next.ID)
	if nextURL == "" {
		return false
	}

	var aAnal *TrackAnalysis
	var bAnal *TrackAnalysis
	beatAligned := fade.autoMix
	if fade.autoMix {
		aAnal = es.analysis
		bAnal = GetCachedAnalysis(guildID, next.ID)
	}
	tag := "Crossfade"
	if beatAligned {
		tag = "AutoMix"
	}

	crossfadeSec := fallbackCrossfadeSec
	if fade.crossfadeSec > 0 {
		crossfadeSec = fade.crossfadeSec
	}
	if beatAligned && aAnal != nil {
		crossfadeSec = float64(fade.autoMixBeats) * aAnal.PeriodSec
		if crossfadeSec < crossfadeMinSec {
			crossfadeSec = crossfadeMinSec
		}
		if crossfadeSec > crossfadeMaxSec {
			crossfadeSec = crossfadeMaxSec
		}
	}

	crossfadeFrames := int(crossfadeSec * framesPerSecond)
	if crossfadeFrames < 1 {
		return false
	}

	bDuration := youtube.ParseDurationToSeconds(next.Duration)
	if bDuration > 0 && float64(bDuration) < crossfadeSec+5 {
		logger.Debugf("[%s] next song too short for crossfade (%ds < %.1fs), skipping for guild: %s", tag, bDuration, crossfadeSec+5, guildID)
		return false
	}

	effectiveEnd := es.totalFrames - es.silentTailFrames
	if es.silentTailFrames > 0 {
		logger.Debugf("[%s] trimming %d silent tail frames, effective end %d of %d for guild: %s", tag, es.silentTailFrames, effectiveEnd, es.totalFrames, guildID)
	}

	maxStart := effectiveEnd - crossfadeFrames
	if maxStart < sentFrames+1 {
		return false
	}

	transitionFrame := effectiveEnd - crossfadeFrames
	if beatAligned {
		transitionFrame -= int(tailMarginSec * framesPerSecond)
		if aAnal != nil {
			transitionFrame = snapTransitionToGrid(transitionFrame, es.tailStartFrame, aAnal)
		}
	}
	if transitionFrame > maxStart {
		transitionFrame = maxStart
	}
	if transitionFrame < sentFrames+1 {
		transitionFrame = sentFrames + 1
	}

	startOffsetSec := 0.0
	if bAnal != nil {
		startOffsetSec = bAnal.FirstBeat
	}

	bArgs := buildFFmpegArgs(nextURL, startOffsetSec, normalization)
	bStream, err := startAudioStream(bArgs, fade.autoMix || fade.trimSilence)
	if err != nil {
		logger.Debugf("[%s] failed to start next stream for guild %s: %v", tag, guildID, err)
		return false
	}

	slideFrames := fallbackSlideFrames
	if aAnal != nil {
		slideFrames = int(math.Round(aAnal.PeriodSec * framesPerSecond))
		if slideFrames < 1 {
			slideFrames = fallbackSlideFrames
		}
	}

	minUsableFrames := minUsableCrossfadeFrames
	if minUsableFrames > crossfadeFrames {
		minUsableFrames = crossfadeFrames
	}

	cs.armed = true
	cs.tag = tag
	cs.trimSilence = fade.trimSilence
	cs.fadeGains = fade.crossfade
	cs.bStream = bStream
	cs.nextSongID = next.ID
	cs.startOffsetSec = startOffsetSec
	cs.transitionFrame = transitionFrame
	cs.crossfadeFrames = crossfadeFrames
	cs.minUsableFrames = minUsableFrames
	cs.totalFrames = effectiveEnd
	cs.slideFrames = slideFrames
	logger.Debugf("[%s] planned crossfade at frame %d (%d frames) into song ID %d for guild: %s", tag, transitionFrame, crossfadeFrames, next.ID, guildID)
	return true
}

func (cs *crossfadeState) bReady() bool {
	return len(cs.bStream.pcmChan) > 0 || cs.bStream.endState.Load() != nil
}

func (cs *crossfadeState) bFailed() bool {
	select {
	case <-cs.bStream.errChan:
		return true
	default:
		return false
	}
}

func (cs *crossfadeState) cancel(reason string) {
	cs.abort()
	cs.armed = false
	cs.cancelled = true
	logger.Debugf("[%s] crossfade cancelled (%s)", cs.tag, reason)
}

func (cs *crossfadeState) pullBFrame() []int16 {
	for {
		select {
		case bf, ok := <-cs.bStream.pcmChan:
			if !ok {
				return nil
			}
			cs.bFramesConsumed++
			if cs.trimSilence && !cs.bLoudSeen {
				if frameSilent(bf) {
					cs.bLeadSkipFrames++
					continue
				}
				cs.bLoudSeen = true
			}
			return bf
		default:
			return nil
		}
	}
}

func (cs *crossfadeState) mixAndSend(player *GuildPlayer, stopCh chan struct{}, aFrame, bFrame []int16, volume float64, enc *OpusEncoder) error {
	aGain := volume
	bGain := volume
	if cs.fadeGains {
		p := float64(cs.mixedFrames) / float64(cs.crossfadeFrames)
		aGain = volume * qsinOut(p)
		bGain = volume * qsinIn(p)
	}

	for i := 0; i < len(cs.mixBuf); i++ {
		var sample float64
		if i < len(aFrame) {
			sample += float64(aFrame[i]) * aGain
		}
		if i < len(bFrame) {
			sample += float64(bFrame[i]) * bGain
		}
		if sample > 32767 {
			cs.mixBuf[i] = 32767
		} else if sample < -32768 {
			cs.mixBuf[i] = -32768
		} else {
			cs.mixBuf[i] = int16(sample)
		}
	}

	opusBuffer := make([]byte, 1500)
	opusLen, err := enc.Encode(cs.mixBuf, opusBuffer)
	if err != nil {
		logger.Errorf("[%s] opus encoding error: %v", cs.tag, err)
		return nil
	}
	opusData := opusBuffer[:opusLen]

	select {
	case player.VoiceConn.OpusSend <- opusData:
		return nil
	case <-player.VoiceConn.Dead:
		return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err)
	case <-stopCh:
		return fmt.Errorf("playback stopped by user")
	}
}

func (cs *crossfadeState) handoff(player *GuildPlayer, enc *OpusEncoder) {
	player.mu.Lock()
	player.PendingStream = &PendingStream{
		SongID:            cs.nextSongID,
		Stream:            cs.bStream,
		Encoder:           enc,
		FramesConsumed:    cs.bFramesConsumed,
		LeadingSkipFrames: cs.bLeadSkipFrames,
		StartOffsetSec:    cs.startOffsetSec,
	}
	player.mu.Unlock()
	cs.handedOff = true
	cs.active = false
	logger.Debugf("[%s] handed off to song ID %d after %d crossfade frames for guild: %s", cs.tag, cs.nextSongID, cs.mixedFrames, player.GuildID)
}

func (cs *crossfadeState) consume(player *GuildPlayer, stopCh chan struct{}, pcmData []int16, volume float64, enc *OpusEncoder, sentFrames *int) (bool, error) {
	if !cs.armed || cs.handedOff {
		return false, nil
	}
	if !cs.active {
		if *sentFrames < cs.transitionFrame {
			return false, nil
		}
		if cs.bFailed() {
			cs.cancel("next stream failed")
			return false, nil
		}
		if !cs.bReady() {
			cs.transitionFrame += cs.slideFrames
			if cs.transitionFrame+cs.crossfadeFrames > cs.totalFrames {
				cs.crossfadeFrames = cs.totalFrames - cs.transitionFrame
				if cs.crossfadeFrames < cs.minUsableFrames {
					cs.cancel("transition window exhausted")
					return false, nil
				}
				logger.Debugf("[%s] crossfade shrunk to %d frames waiting for next stream", cs.tag, cs.crossfadeFrames)
			}
			logger.Debugf("[%s] next stream not ready, deferred transition to frame %d", cs.tag, cs.transitionFrame)
			return false, nil
		}
		cs.active = true
	}

	bFrame := cs.pullBFrame()
	if err := cs.mixAndSend(player, stopCh, pcmData, bFrame, volume, enc); err != nil {
		cs.abort()
		return true, err
	}
	*sentFrames++
	cs.mixedFrames++

	if cs.mixedFrames >= cs.crossfadeFrames {
		cs.handoff(player, enc)
		return true, nil
	}
	return false, nil
}

func (cs *crossfadeState) finishOnDrain(player *GuildPlayer, stopCh chan struct{}, enc *OpusEncoder, sentFrames *int) (bool, error) {
	if !cs.armed || cs.handedOff {
		return false, nil
	}
	if !cs.active {
		cs.cancel("source drained before transition")
		return false, nil
	}

	player.mu.Lock()
	volume := player.Volume
	player.mu.Unlock()

	for cs.mixedFrames < cs.crossfadeFrames {
		bFrame := cs.pullBFrame()
		if err := cs.mixAndSend(player, stopCh, nil, bFrame, volume, enc); err != nil {
			cs.abort()
			return true, err
		}
		*sentFrames++
		cs.mixedFrames++
	}

	cs.handoff(player, enc)
	return true, nil
}

func (cs *crossfadeState) abort() {
	if cs.bStream != nil && !cs.handedOff {
		cs.bStream.stop()
	}
}
