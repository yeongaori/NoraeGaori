package player

import (
	"fmt"
	"math"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"
	"strings"
	"sync/atomic"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

const (
	tailMarginSec            = 4.0
	minUsableCrossfadeFrames = 100
	fallbackSlideFrames      = 25
)

type PendingStream struct {
	SongID            int
	Stream            audioStream
	Encoder           *opus.Encoder
	FramesConsumed    int
	LeadingSkipFrames int
	StartOffsetSec    float64
	Tail              *transition.Tail
}

type crossfadeState struct {
	armed           bool
	active          bool
	handedOff       bool
	cancelled       bool
	trimSilence     bool
	bLoudSeen       bool
	fadeGains       bool
	autoMix         bool
	scope           *logger.Scoped
	bStream         audioStream
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
	opusScratch     []byte
	recipe          transition.Recipe
	processor       *transition.Processor
	loopFrames      int
	loopBuffer      [][]int16
	loopBacking     []int16
	loopIndex       int
	guildID         string
	normalization   bool
	bitrate         int
	bRetried        bool
	bRefetch        atomic.Pointer[streamRef]
	bRefetching     atomic.Bool
	bAborted        atomic.Bool
}

func newCrossfadeState() *crossfadeState {
	return &crossfadeState{
		autoMix:     true,
		scope:       logger.Scope("AutoMix"),
		mixBuf:      make([]int16, frameSize*channels),
		opusScratch: make([]byte, maxOpusFrameBytes),
		recipe:      transition.DefaultRecipe(),
	}
}

func snapTransitionToGrid(target, tailStartFrame int, a *analysis.TrackAnalysis) int {
	if a.PeriodSec <= 0 {
		return target
	}
	periodFrames := a.PeriodSec * dsp.FramesPerSecond
	firstBeatFrame := float64(tailStartFrame) + a.FirstBeat*dsp.FramesPerSecond
	k := math.Round((float64(target) - firstBeatFrame) / periodFrames)
	return int(math.Round(firstBeatFrame + k*periodFrames))
}

func snapTransitionToBar(target, tailStartFrame int, a *analysis.TrackAnalysis) int {
	if a.PeriodSec <= 0 {
		return target
	}
	periodFrames := a.PeriodSec * dsp.FramesPerSecond
	firstBeatFrame := float64(tailStartFrame) + a.FirstBeat*dsp.FramesPerSecond
	beat := math.Round((float64(target) - firstBeatFrame) / periodFrames)
	phase := float64(a.DownbeatPhase)
	bars := math.Round((beat - phase) / analysis.BarBeats)
	beat = phase + bars*analysis.BarBeats
	return int(math.Round(firstBeatFrame + beat*periodFrames))
}

func nextCrossfadeCandidate(guildID string) (*queue.Song, *queue.Song, string) {
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) < 2 {
		return nil, nil, ""
	}
	if q.Songs[0].IsLive || q.Songs[1].IsLive {
		return nil, nil, ""
	}

	nextURL := GetCachedStreamURL(guildID, q.Songs[1].ID)
	if nextURL == "" {
		return nil, nil, ""
	}

	return q.Songs[0], q.Songs[1], nextURL
}

func analysisPeriodSec(a *analysis.TrackAnalysis) float64 {
	if a == nil {
		return 0
	}
	return a.PeriodSec
}

func analysisFirstBeat(a *analysis.TrackAnalysis) float64 {
	if a == nil {
		return 0
	}
	return a.FirstBeat
}

func resolveSlideFrames(a *analysis.TrackAnalysis) int {
	if a == nil {
		return fallbackSlideFrames
	}

	slideFrames := int(math.Round(a.PeriodSec * dsp.FramesPerSecond))
	if slideFrames < 1 {
		return fallbackSlideFrames
	}
	return slideFrames
}

func resolveTransitionFrame(transitionFrame, maxStart, sentFrames, tailStartFrame int, beatAligned bool, loopBeats int, a *analysis.TrackAnalysis) int {
	if beatAligned {
		transitionFrame -= int(tailMarginSec * dsp.FramesPerSecond)
		transitionFrame = snapTransitionToBeats(transitionFrame, tailStartFrame, loopBeats, a)
	}
	if transitionFrame > maxStart {
		transitionFrame = maxStart
	}
	if transitionFrame < sentFrames+1 {
		return sentFrames + 1
	}
	return transitionFrame
}

func snapTransitionToBeats(transitionFrame, tailStartFrame, loopBeats int, a *analysis.TrackAnalysis) int {
	if a == nil {
		return transitionFrame
	}
	if loopBeats >= analysis.BarBeats {
		return snapTransitionToBar(transitionFrame, tailStartFrame, a)
	}
	return snapTransitionToGrid(transitionFrame, tailStartFrame, a)
}

type crossfadePlan struct {
	autoMix         bool
	scope           *logger.Scoped
	guildID         string
	normalization   bool
	bitrate         int
	trimSilence     bool
	fadeGains       bool
	bStream         audioStream
	nextSongID      int
	startOffsetSec  float64
	transitionFrame int
	crossfadeFrames int
	minUsableFrames int
	totalFrames     int
	slideFrames     int
	recipe          transition.Recipe
	loopFrames      int
	periodSec       float64
	flatGains       bool
	description     string
}

func (cs *crossfadeState) buildPlan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, fade fadeSettings, normalization bool, bitrate int) *crossfadePlan {
	if (!fade.autoMix && !fade.crossfade) || cs.armed {
		return nil
	}
	if fade.repeatMode == queue.RepeatSingle {
		return nil
	}

	var aAnal *analysis.TrackAnalysis
	beatAligned := fade.autoMix
	if fade.autoMix {
		aAnal = es.Analysis
	}

	crossfadeFrames, crossfadeSec := transition.CrossfadeFrames(fade.autoMix, fade.autoMixBeats, fade.crossfadeSec, aAnal)
	if crossfadeFrames < 1 {
		return nil
	}

	effectiveEnd := es.TotalFrames - es.SilentTailFrames
	maxStart := effectiveEnd - crossfadeFrames
	if maxStart < sentFrames+1 {
		return nil
	}

	guildID := player.GuildID
	current, next, nextURL := nextCrossfadeCandidate(guildID)
	if next == nil {
		return nil
	}

	var bAnal *analysis.TrackAnalysis
	if fade.autoMix {
		bAnal = LookupAnalysis(guildID, next, analysis.SegmentHead)
	}
	scope := logger.Scope("Crossfade")
	if beatAligned {
		scope = logger.Scope("AutoMix")
	}

	songOverrides := songTransitionOverrides(current)
	recipe, _, styleSource := transition.ResolveStyles(aAnal, bAnal, fade.autoMix, fade.styleOverrides, songOverrides)

	periodSec := analysisPeriodSec(aAnal)

	loopStyle, loopFrames := transition.ClampLoopStyle(recipe.Loop, periodSec, crossfadeFrames)
	recipe.Loop = loopStyle
	loopBeats := transition.LoopBeatCount(recipe.Loop)

	bDuration := youtube.ParseDurationToSeconds(next.Duration)
	if bDuration > 0 && float64(bDuration) < crossfadeSec+5 {
		scope.Debugf("next song too short for crossfade (%ds < %.1fs), skipping for guild: %s", bDuration, crossfadeSec+5, guildID)
		return nil
	}

	if es.SilentTailFrames > 0 {
		scope.Debugf("trimming %d silent tail frames, effective end %d of %d for guild: %s", es.SilentTailFrames, effectiveEnd, es.TotalFrames, guildID)
	}

	transitionFrame := resolveTransitionFrame(effectiveEnd-crossfadeFrames, maxStart, sentFrames, es.TailStartFrame, beatAligned, loopBeats, aAnal)
	startOffsetSec := analysisFirstBeat(bAnal)

	bArgs := ffmpeg.Args(nextURL, startOffsetSec, normalization)
	bStream, err := newAudioStream(bArgs, fade.autoMix || fade.trimSilence)
	if err != nil {
		scope.Debugf("failed to start next stream for guild %s: %v", guildID, err)
		return nil
	}

	slideFrames := resolveSlideFrames(aAnal)
	minUsableFrames := min(minUsableCrossfadeFrames, crossfadeFrames)

	return &crossfadePlan{
		autoMix:         beatAligned,
		scope:           scope,
		guildID:         guildID,
		normalization:   normalization,
		bitrate:         bitrate,
		trimSilence:     fade.trimSilence,
		fadeGains:       fade.crossfade,
		bStream:         bStream,
		nextSongID:      next.ID,
		startOffsetSec:  startOffsetSec,
		transitionFrame: transitionFrame,
		crossfadeFrames: crossfadeFrames,
		minUsableFrames: minUsableFrames,
		totalFrames:     effectiveEnd,
		slideFrames:     slideFrames,
		recipe:          recipe,
		loopFrames:      loopFrames,
		periodSec:       periodSec,
		flatGains:       !fade.crossfade,
		description:     fmt.Sprintf("recipe %s (%s) [%s]", recipe, describeTransitionInputs(aAnal, bAnal), describeStyleSources(styleSource)),
	}
}

func (cs *crossfadeState) commit(p *crossfadePlan) {
	cs.armed = true
	cs.autoMix = p.autoMix
	cs.scope = p.scope
	cs.guildID = p.guildID
	cs.normalization = p.normalization
	cs.bitrate = p.bitrate
	cs.trimSilence = p.trimSilence
	cs.fadeGains = p.fadeGains
	cs.bStream = p.bStream
	cs.nextSongID = p.nextSongID
	cs.startOffsetSec = p.startOffsetSec
	cs.transitionFrame = p.transitionFrame
	cs.crossfadeFrames = p.crossfadeFrames
	cs.minUsableFrames = p.minUsableFrames
	cs.totalFrames = p.totalFrames
	cs.slideFrames = p.slideFrames
	cs.recipe = p.recipe
	cs.loopFrames = p.loopFrames
	cs.loopBuffer = nil
	cs.loopBacking = nil
	cs.loopIndex = 0
	cs.processor = transition.NewProcessor(p.recipe, p.crossfadeFrames, p.periodSec)
	cs.processor.SetFlatGains(p.flatGains)
}

func (cs *crossfadeState) plan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, fade fadeSettings, normalization bool, bitrate int) bool {
	p := cs.buildPlan(player, es, sentFrames, fade, normalization, bitrate)
	if p == nil {
		return false
	}

	cs.commit(p)

	p.scope.Debugf("planned crossfade at frame %d (%d frames) into song ID %d for guild: %s", p.transitionFrame, p.crossfadeFrames, p.nextSongID, p.guildID)
	p.scope.Debugf("%s for guild: %s", p.description, p.guildID)
	return true
}

func songTransitionOverrides(song *queue.Song) transition.StyleOverrides {
	if song == nil {
		return transition.StyleOverrides{}
	}
	return transition.StyleOverrides{
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

func describeTransitionInputs(a, b *analysis.TrackAnalysis) string {
	if a == nil || b == nil {
		return "analysis unavailable"
	}
	raw := math.Abs(b.BPM-a.BPM) / a.BPM
	folded, factor := analysis.TempoDeltaFactor(a.BPM, b.BPM)
	return fmt.Sprintf("bpmA=%.1f bpmB=%.1f delta=%.4f raw=%.4f factorB=%.1fx keyA=%s keyB=%s confA=%.4f confB=%.4f distance=%d",
		a.BPM, b.BPM, folded, raw, factor,
		analysis.CamelotCode(a.Tonic, a.Minor), analysis.CamelotCode(b.Tonic, b.Minor),
		a.KeyConfidence, b.KeyConfidence, analysis.CamelotDistance(a, b))
}

func (cs *crossfadeState) bReady() bool {
	return len(cs.bStream.PCM()) > 0 || cs.bStream.EndState() != nil
}

func (cs *crossfadeState) bFailed() bool {
	select {
	case <-cs.bStream.Errs():
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
		cs.scope.Debugf("crossfade shrunk to %d frames waiting for next stream", cs.crossfadeFrames)
	}
	cs.scope.Debugf("%s, deferred transition to frame %d", reason, cs.transitionFrame)
}

func (cs *crossfadeState) startNextStreamRefetch(player *GuildPlayer) {
	guildID := cs.guildID
	songID := cs.nextSongID
	startOffsetSec := cs.startOffsetSec
	normalization := cs.normalization
	bitrate := cs.bitrate
	collectTail := cs.trimSilence || cs.autoMix

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
			cs.scope.Debugf("refetch failed for song ID %d in guild %s: %v", songID, guildID, err)
			return
		}

		stream, err := newAudioStream(ffmpeg.Args(freshURL, startOffsetSec, normalization), collectTail)
		if err != nil {
			cs.scope.Debugf("refetched stream failed to start for guild %s: %v", guildID, err)
			return
		}
		if cs.bAborted.Load() {
			stream.Stop()
			return
		}
		if previous := cs.bRefetch.Swap(&streamRef{stream: stream}); previous != nil {
			previous.stream.Stop()
		}
		if cs.bAborted.Load() {
			if orphan := cs.bRefetch.Swap(nil); orphan != nil {
				orphan.stream.Stop()
			}
		}
	}()
}

func (cs *crossfadeState) cancel(reason string) {
	cs.abort()
	cs.armed = false
	cs.cancelled = true
	cs.scope.Debugf("crossfade cancelled (%s)", reason)
}

func (cs *crossfadeState) pullBFrame() []int16 {
	for {
		select {
		case bf, ok := <-cs.bStream.PCM():
			if !ok {
				return nil
			}
			cs.bFramesConsumed++
			if cs.trimSilence && !cs.bLoudSeen {
				if dsp.FrameSilent(bf) {
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

func (cs *crossfadeState) mixAndSend(player *GuildPlayer, stopCh chan struct{}, aFrame, bFrame []int16, volume float64, enc *opus.Encoder) error {
	progress := 0.0
	if cs.crossfadeFrames > 0 {
		progress = float64(cs.mixedFrames) / float64(cs.crossfadeFrames)
	}

	if cs.processor != nil {
		aBuf := cs.processor.ProcessA(aFrame, progress)
		bBuf := cs.processor.ProcessB(bFrame, progress)
		cs.processor.ApplyGains(aBuf, bBuf, progress, volume)

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
			aGain = volume * dsp.QSinOut(progress)
			bGain = volume * dsp.QSinIn(progress)
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

	opusLen, err := enc.Encode(cs.mixBuf, cs.opusScratch)
	if err != nil {
		cs.scope.Errorf("opus encoding error: %v", err)
		return nil
	}
	opusData := make([]byte, opusLen)
	copy(opusData, cs.opusScratch[:opusLen])

	select {
	case player.VoiceConn.OpusSendChan() <- opusData:
		return nil
	case <-player.VoiceConn.DeadChan():
		return fmt.Errorf("voice connection died: %v", player.VoiceConn.Err())
	case <-stopCh:
		return fmt.Errorf("playback stopped by user")
	}
}

func (cs *crossfadeState) handoff(player *GuildPlayer, enc *opus.Encoder) {
	var tail *transition.Tail
	if cs.processor != nil {
		tail = cs.processor.MakeTail(cs.processor.LastGain())
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
	cs.scope.Debugf("handed off to song ID %d after %d crossfade frames for guild: %s", cs.nextSongID, cs.mixedFrames, player.GuildID)
}

func (cs *crossfadeState) consume(player *GuildPlayer, stopCh chan struct{}, pcmData []int16, volume float64, enc *opus.Encoder, sentFrames *int) (bool, error) {
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
				cs.bStream = refetched.stream
				cs.scope.Debugf("next stream reopened with a fresh URL for guild: %s", cs.guildID)
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

func (cs *crossfadeState) finishOnDrain(player *GuildPlayer, stopCh chan struct{}, enc *opus.Encoder, sentFrames *int) (bool, error) {
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
		if cs.loopBacking == nil {
			cs.loopBacking = make([]int16, cs.loopFrames*frameSize*channels)
			cs.loopBuffer = make([][]int16, 0, cs.loopFrames)
		}
		start := len(cs.loopBuffer) * frameSize * channels
		end := start + frameSize*channels
		stored := cs.loopBacking[start:end:end]
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
		cs.bStream.Stop()
	}
	if pending := cs.bRefetch.Swap(nil); pending != nil {
		pending.stream.Stop()
	}
}
