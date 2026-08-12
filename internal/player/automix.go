package player

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"

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
	Tail              *transitionTail
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
	recipe          TransitionRecipe
	processor       *transitionProcessor
	loopFrames      int
	loopBuffer      [][]int16
	loopIndex       int
	guildID         string
	normalization   bool
	bitrate         int
	bRetried        bool
	bRefetch        atomic.Pointer[audioStream]
	bRefetching     atomic.Bool
	bAborted        atomic.Bool
}

func newCrossfadeState() *crossfadeState {
	return &crossfadeState{
		tag:    "AutoMix",
		mixBuf: make([]int16, frameSize*channels),
		recipe: defaultTransitionRecipe(),
	}
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

func snapTransitionToBar(target, tailStartFrame int, a *TrackAnalysis) int {
	if a.PeriodSec <= 0 {
		return target
	}
	periodFrames := a.PeriodSec * framesPerSecond
	firstBeatFrame := float64(tailStartFrame) + a.FirstBeat*framesPerSecond
	beat := math.Round((float64(target) - firstBeatFrame) / periodFrames)
	phase := float64(a.DownbeatPhase)
	bars := math.Round((beat - phase) / keyBarBeats)
	beat = phase + bars*keyBarBeats
	return int(math.Round(firstBeatFrame + beat*periodFrames))
}

func (cs *crossfadeState) plan(player *GuildPlayer, es *streamEndState, sentFrames int, fade fadeSettings, normalization bool, bitrate int) bool {
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
	if q.Songs[0].IsLive {
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
		bAnal = LookupAnalysis(guildID, next, AnalysisSegmentHead)
	}
	tag := "Crossfade"
	if beatAligned {
		tag = "AutoMix"
	}

	crossfadeFrames, crossfadeSec := TransitionCrossfadeFrames(fade.autoMix, fade.autoMixBeats, fade.crossfadeSec, aAnal)
	if crossfadeFrames < 1 {
		return false
	}

	songOverrides := songTransitionOverrides(q.Songs[0])
	recipe, _, styleSource := ResolveTransitionStyles(aAnal, bAnal, fade.autoMix, fade.styleOverrides, songOverrides)

	periodSec := 0.0
	if aAnal != nil {
		periodSec = aAnal.PeriodSec
	}

	loopStyle, loopFrames := ClampLoopStyle(recipe.Loop, periodSec, crossfadeFrames)
	recipe.Loop = loopStyle
	loopBeats := loopBeatCount(recipe.Loop)

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
			if loopBeats >= keyBarBeats {
				transitionFrame = snapTransitionToBar(transitionFrame, es.tailStartFrame, aAnal)
			} else {
				transitionFrame = snapTransitionToGrid(transitionFrame, es.tailStartFrame, aAnal)
			}
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
	bStream, err := newAudioStream(bArgs, fade.autoMix || fade.trimSilence)
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
	cs.guildID = guildID
	cs.normalization = normalization
	cs.bitrate = bitrate
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
	cs.recipe = recipe
	cs.loopFrames = loopFrames
	cs.loopBuffer = nil
	cs.loopIndex = 0
	cs.processor = newTransitionProcessor(recipe, crossfadeFrames, periodSec)
	cs.processor.flatGains = !fade.crossfade

	logger.Debugf("[%s] planned crossfade at frame %d (%d frames) into song ID %d for guild: %s", tag, transitionFrame, crossfadeFrames, next.ID, guildID)
	logger.Debugf("[%s] recipe %s (%s) [%s] for guild: %s", tag, recipe,
		describeTransitionInputs(aAnal, bAnal),
		describeStyleSources(styleSource), guildID)
	return true
}

func songTransitionOverrides(song *queue.Song) TransitionStyleOverrides {
	if song == nil {
		return TransitionStyleOverrides{}
	}
	return TransitionStyleOverrides{
		Volume: song.AutoMixStyleVolume,
		EQ:     song.AutoMixStyleEQ,
		Filter: song.AutoMixStyleFilter,
		Effect: song.AutoMixStyleEffect,
		Loop:   song.AutoMixStyleLoop,
	}
}

func describeStyleSources(source map[string]string) string {
	parts := make([]string, 0, len(source))
	for _, category := range []string{"volume", "eq", "filter", "effect", "loop"} {
		parts = append(parts, fmt.Sprintf("%s:%s", category, source[category]))
	}
	return strings.Join(parts, " ")
}

func describeTransitionInputs(a, b *TrackAnalysis) string {
	if a == nil || b == nil {
		return "analysis unavailable"
	}
	raw := math.Abs(b.BPM-a.BPM) / a.BPM
	folded, factor := tempoDeltaFactor(a.BPM, b.BPM)
	return fmt.Sprintf("bpmA=%.1f bpmB=%.1f delta=%.4f raw=%.4f factorB=%.1fx keyA=%s keyB=%s confA=%.4f confB=%.4f distance=%d",
		a.BPM, b.BPM, folded, raw, factor,
		camelotCode(a.Tonic, a.Minor), camelotCode(b.Tonic, b.Minor),
		a.KeyConfidence, b.KeyConfidence, camelotDistance(a, b))
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

func (cs *crossfadeState) slideTransition(reason string) {
	cs.transitionFrame += cs.slideFrames
	if cs.transitionFrame+cs.crossfadeFrames > cs.totalFrames {
		cs.crossfadeFrames = cs.totalFrames - cs.transitionFrame
		if cs.crossfadeFrames < cs.minUsableFrames {
			cs.cancel("transition window exhausted")
			return
		}
		logger.Debugf("[%s] crossfade shrunk to %d frames waiting for next stream", cs.tag, cs.crossfadeFrames)
	}
	logger.Debugf("[%s] %s, deferred transition to frame %d", cs.tag, reason, cs.transitionFrame)
}

func (cs *crossfadeState) startNextStreamRefetch(player *GuildPlayer) {
	guildID := cs.guildID
	songID := cs.nextSongID
	startOffsetSec := cs.startOffsetSec
	normalization := cs.normalization
	bitrate := cs.bitrate
	collectTail := cs.trimSilence || cs.tag == "AutoMix"

	invalidatePreCacheSong(guildID, songID)
	cs.bRefetching.Store(true)

	go func() {
		defer cs.bRefetching.Store(false)

		q, err := queue.GetQueue(guildID, false)
		if err != nil || q == nil {
			return
		}
		var next *queue.Song
		for _, candidate := range q.Songs {
			if candidate.ID == songID {
				next = candidate
				break
			}
		}
		if next == nil {
			return
		}

		freshURL, err := youtube.GetStreamURL(next.URL, q.SponsorBlock, bitrate)
		if err != nil {
			logger.Debugf("[%s] refetch failed for song ID %d in guild %s: %v", cs.tag, songID, guildID, err)
			return
		}

		stream, err := newAudioStream(buildFFmpegArgs(freshURL, startOffsetSec, normalization), collectTail)
		if err != nil {
			logger.Debugf("[%s] refetched stream failed to start for guild %s: %v", cs.tag, guildID, err)
			return
		}
		if cs.bAborted.Load() {
			stream.stop()
			return
		}
		if previous := cs.bRefetch.Swap(stream); previous != nil {
			previous.stop()
		}
		if cs.bAborted.Load() {
			if orphan := cs.bRefetch.Swap(nil); orphan != nil {
				orphan.stop()
			}
		}
	}()
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
	progress := 0.0
	if cs.crossfadeFrames > 0 {
		progress = float64(cs.mixedFrames) / float64(cs.crossfadeFrames)
	}

	if cs.processor != nil {
		aBuf := cs.processor.processA(aFrame, progress)
		bBuf := cs.processor.processB(bFrame, progress)
		cs.processor.applyGains(aBuf, bBuf, progress, volume)

		for i := 0; i < len(cs.mixBuf); i++ {
			sample := aBuf[i] + bBuf[i]
			if sample > 32767 {
				cs.mixBuf[i] = 32767
			} else if sample < -32768 {
				cs.mixBuf[i] = -32768
			} else {
				cs.mixBuf[i] = int16(sample)
			}
		}
	} else {
		aGain := volume
		bGain := volume
		if cs.fadeGains {
			aGain = volume * qsinOut(progress)
			bGain = volume * qsinIn(progress)
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
	}

	opusBuffer := make([]byte, 1500)
	opusLen, err := enc.Encode(cs.mixBuf, opusBuffer)
	if err != nil {
		logger.Errorf("[%s] opus encoding error: %v", cs.tag, err)
		return nil
	}
	opusData := opusBuffer[:opusLen]

	select {
	case player.VoiceConn.OpusSendChan() <- opusData:
		return nil
	case <-player.VoiceConn.DeadChan():
		return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err())
	case <-stopCh:
		return fmt.Errorf("playback stopped by user")
	}
}

func (cs *crossfadeState) handoff(player *GuildPlayer, enc *OpusEncoder) {
	var tail *transitionTail
	if cs.processor != nil {
		tail = cs.processor.makeTail(cs.processor.previousAGain)
	}

	player.mu.Lock()
	player.PendingStream = &PendingStream{
		SongID:            cs.nextSongID,
		Stream:            cs.bStream,
		Encoder:           enc,
		FramesConsumed:    cs.bFramesConsumed,
		LeadingSkipFrames: cs.bLeadSkipFrames,
		StartOffsetSec:    cs.startOffsetSec,
		Tail:              tail,
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
			if !cs.bRetried {
				cs.bRetried = true
				cs.startNextStreamRefetch(player)
			}
			if refetched := cs.bRefetch.Swap(nil); refetched != nil {
				cs.bStream = refetched
				logger.Debugf("[%s] next stream reopened with a fresh URL for guild: %s", cs.tag, cs.guildID)
			} else if cs.bRefetching.Load() {
				cs.slideTransition("next stream refetching")
				return false, nil
			} else {
				cs.cancel("next stream failed")
				return false, nil
			}
		}
		if !cs.bReady() {
			cs.slideTransition("next stream not ready")
			return false, nil
		}
		cs.active = true
	}

	aFrame := pcmData
	if cs.loopFrames > 0 {
		aFrame = cs.loopFrame(pcmData)
	}

	bFrame := cs.pullBFrame()
	if err := cs.mixAndSend(player, stopCh, aFrame, bFrame, volume, enc); err != nil {
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
		var aFrame []int16
		if cs.loopFrames > 0 && len(cs.loopBuffer) >= cs.loopFrames {
			aFrame = cs.loopFrame(nil)
		}

		bFrame := cs.pullBFrame()
		if err := cs.mixAndSend(player, stopCh, aFrame, bFrame, volume, enc); err != nil {
			cs.abort()
			return true, err
		}
		*sentFrames++
		cs.mixedFrames++
	}

	cs.handoff(player, enc)
	return true, nil
}

func (cs *crossfadeState) loopFrame(pcmData []int16) []int16 {
	if len(cs.loopBuffer) < cs.loopFrames {
		if pcmData == nil {
			return nil
		}
		stored := make([]int16, len(pcmData))
		copy(stored, pcmData)
		cs.loopBuffer = append(cs.loopBuffer, stored)
		return stored
	}

	frame := cs.loopBuffer[cs.loopIndex]
	cs.loopIndex++
	if cs.loopIndex >= len(cs.loopBuffer) {
		cs.loopIndex = 0
	}
	return frame
}

func (cs *crossfadeState) abort() {
	cs.bAborted.Store(true)
	if cs.bStream != nil && !cs.handedOff {
		cs.bStream.stop()
	}
	if pending := cs.bRefetch.Swap(nil); pending != nil {
		pending.stop()
	}
}
