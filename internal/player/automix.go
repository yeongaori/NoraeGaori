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
	minUsableCrossfadeFrames = 100
	fallbackSlideFrames      = 25
	transitionLeadSec        = 2.0
	minNextSongMarginSec     = 5.0
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
	trimBLead       bool
	bLoudSeen       bool
	autoMix         bool
	scope           *logger.Scoped
	bStream         audioStream
	nextSongID      int
	bSeekSec        float64
	bTempo          ffmpeg.Tempo
	transitionFrame int
	crossfadeFrames int
	minUsableFrames int
	totalFrames     int
	slideFrames     int
	mixedFrames     int
	bFramesConsumed int
	bLeadSkipFrames int
	mixBuf          []int16
	mixFloat        []float64
	limiter         dsp.Limiter
	opusScratch     []byte
	processor       *transition.Processor
	beatLoop        *transition.BeatLoop
	bLoop           *transition.BeatLoop
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
		mixFloat:    make([]float64, frameSize*channels),
		opusScratch: make([]byte, maxOpusFrameBytes),
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

func resolveSlideFrames(a *analysis.TrackAnalysis, beatmatched bool) int {
	if a == nil {
		return fallbackSlideFrames
	}

	beats := 1.0
	if beatmatched {
		beats = analysis.BarBeats
	}
	slideFrames := int(math.Round(beats * a.PeriodSec * dsp.FramesPerSecond))
	if slideFrames < 1 {
		return fallbackSlideFrames
	}
	return slideFrames
}

type transitionPlacement struct {
	transitionFrame int
	crossfadeFrames int
	bSeekSec        float64
	bTempo          ffmpeg.Tempo
	bars            int
	beatmatched     bool
	auto            *transition.Recipe
	summary         string
}

func placeCrossfade(fade *fadeSettings, effectiveEnd, sentFrames int, nextSec float64) (transitionPlacement, bool) {
	crossfadeFrames, crossfadeSec := transition.CrossfadeFrames(false, 0, fade.crossfadeSec, nil)
	transitionFrame := effectiveEnd - crossfadeFrames
	if crossfadeFrames < 1 || transitionFrame < sentFrames+1 {
		return transitionPlacement{}, false
	}
	if nextSec > 0 && nextSec < crossfadeSec+minNextSongMarginSec {
		return transitionPlacement{}, false
	}
	return transitionPlacement{
		transitionFrame: transitionFrame,
		crossfadeFrames: crossfadeFrames,
		auto:            transition.PresetRecipe(transition.NoPreset),
		summary:         "crossfade",
	}, true
}

func placeAutoMix(pair *transition.Pair, originSec float64, effectiveEnd int) (transitionPlacement, bool) {
	overlap, ok := transition.SelectOverlap(pair)
	if !ok {
		return transitionPlacement{}, false
	}

	startFrames := (overlap.StartA - originSec) * dsp.FramesPerSecond
	transitionFrame := int(math.Floor(startFrames))
	endFrame := min(effectiveEnd, int(math.Round((overlap.StartA+overlap.Length-originSec)*dsp.FramesPerSecond)))
	if endFrame-transitionFrame < 1 {
		return transitionPlacement{}, false
	}

	placement := transitionPlacement{
		transitionFrame: transitionFrame,
		crossfadeFrames: endFrame - transitionFrame,
		bars:            overlap.Bars,
		auto:            transition.PresetRecipe(overlap.Preset),
		summary:         overlap.String(),
	}
	if overlap.Beatmatched {
		subFrameSec := (startFrames - float64(transitionFrame)) / dsp.FramesPerSecond
		placement.bSeekSec = math.Max(0, overlap.StartB-subFrameSec*overlap.SpeedB)
		placement.bTempo = ffmpeg.Tempo{Speed: overlap.SpeedB, HoldSec: overlap.Length + ffmpeg.TempoSettleSec}
		placement.beatmatched = true
	}
	return placement, true
}

func songSeconds(song *queue.Song) float64 {
	return float64(youtube.ParseDurationToSeconds(song.Duration))
}

type crossfadePlan struct {
	autoMix         bool
	scope           *logger.Scoped
	guildID         string
	normalization   bool
	bitrate         int
	trimSilence     bool
	trimBLead       bool
	bStream         audioStream
	nextSongID      int
	bSeekSec        float64
	bTempo          ffmpeg.Tempo
	transitionFrame int
	crossfadeFrames int
	minUsableFrames int
	totalFrames     int
	slideFrames     int
	resolved        *transition.Resolved
	window          transition.Window
	description     string
}

func (cs *crossfadeState) buildPlan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, originSec float64, fade *fadeSettings, normalization bool, bitrate int) *crossfadePlan {
	if (!fade.autoMix && !fade.crossfade) || cs.armed {
		return nil
	}
	if fade.repeatMode == queue.RepeatSingle {
		return nil
	}

	guildID := player.GuildID
	current, next, nextURL := nextCrossfadeCandidate(guildID)
	if next == nil {
		return nil
	}

	effectiveEnd := es.TotalFrames - es.SilentTailFrames
	scope := logger.Scope("Crossfade")
	var aAnal, bAnal *analysis.TrackAnalysis
	var placement transitionPlacement
	var ok bool
	if fade.autoMix {
		scope = logger.Scope("AutoMix")
		aAnal = es.Analysis
		bAnal = LookupAnalysis(guildID, next, analysis.SegmentHead)
		placement, ok = placeAutoMix(&transition.Pair{
			From: transition.Track{
				URL:      current.URL,
				Duration: songSeconds(current),
				End:      originSec + float64(effectiveEnd)/dsp.FramesPerSecond,
				Analysis: aAnal,
			},
			To:            transition.Track{URL: next.URL, Duration: songSeconds(next), Analysis: bAnal},
			MaxBeats:      fade.autoMixBeats,
			EarliestStart: originSec + float64(sentFrames)/dsp.FramesPerSecond + transitionLeadSec,
			Settings:      transition.ResolveSettings(songOverrides(current)),
		}, originSec, effectiveEnd)
	} else {
		placement, ok = placeCrossfade(fade, effectiveEnd, sentFrames, songSeconds(next))
	}
	if !ok {
		return nil
	}

	if es.SilentTailFrames > 0 {
		scope.Debugf("trimming %d silent tail frames, effective end %d of %d for guild: %s", es.SilentTailFrames, effectiveEnd, es.TotalFrames, guildID)
	}

	resolved := transition.ResolveStyles(placement.auto, fade.styleOverrides, songOverrides(current))
	resolved.ClampRolls(analysisPeriodSec(aAnal), placement.crossfadeFrames)

	bArgs := ffmpeg.Args(nextURL, placement.bSeekSec, normalization, placement.bTempo)
	bStream, err := player.startStream(bArgs, fade.autoMix || fade.trimSilence)
	if err != nil {
		scope.Debugf("failed to start next stream for guild %s: %v", guildID, err)
		return nil
	}

	return &crossfadePlan{
		autoMix:         fade.autoMix,
		scope:           scope,
		guildID:         guildID,
		normalization:   normalization,
		bitrate:         bitrate,
		trimSilence:     fade.trimSilence,
		trimBLead:       fade.trimSilence && !placement.beatmatched,
		bStream:         bStream,
		nextSongID:      next.ID,
		bSeekSec:        placement.bSeekSec,
		bTempo:          placement.bTempo,
		transitionFrame: placement.transitionFrame,
		crossfadeFrames: placement.crossfadeFrames,
		minUsableFrames: min(minUsableCrossfadeFrames, placement.crossfadeFrames),
		totalFrames:     effectiveEnd,
		slideFrames:     resolveSlideFrames(aAnal, placement.beatmatched),
		resolved:        resolved,
		window: transition.Window{
			Frames:    placement.crossfadeFrames,
			PeriodSec: analysisPeriodSec(aAnal),
			Bars:      placement.bars,
		},
		description: fmt.Sprintf("%s recipe %s (%s) [%s]", placement.summary, &resolved.Recipe,
			describeTransitionInputs(aAnal, bAnal), describeStyleSources(resolved.Sources)),
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
	cs.trimBLead = p.trimBLead
	cs.bStream = p.bStream
	cs.nextSongID = p.nextSongID
	cs.bSeekSec = p.bSeekSec
	cs.bTempo = p.bTempo
	cs.transitionFrame = p.transitionFrame
	cs.crossfadeFrames = p.crossfadeFrames
	cs.minUsableFrames = p.minUsableFrames
	cs.totalFrames = p.totalFrames
	cs.slideFrames = p.slideFrames
	recipe := &p.resolved.Recipe
	cs.beatLoop = transition.PrepareBeatLoop(recipe.Loop, p.window.PeriodSec, p.window.Frames)
	cs.bLoop = transition.PrepareIncomingLoop(recipe.In.FX, p.window.PeriodSec, p.window.Frames)
	cs.limiter = dsp.Limiter{}
	cs.processor = transition.NewProcessor(recipe, &p.window)
}

func (cs *crossfadeState) plan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, originSec float64, fade *fadeSettings, normalization bool, bitrate int) bool {
	p := cs.buildPlan(player, es, sentFrames, originSec, fade, normalization, bitrate)
	if p == nil {
		return false
	}

	cs.commit(p)

	p.scope.Debugf("planned crossfade at frame %d (%d frames) into song ID %d for guild: %s", p.transitionFrame, p.crossfadeFrames, p.nextSongID, p.guildID)
	p.scope.Debugf("%s for guild: %s", p.description, p.guildID)
	return true
}

func songOverrides(song *queue.Song) map[string]string {
	if song == nil {
		return nil
	}
	return song.AutoMixOverrides
}

func describeStyleSources(source map[transition.Category]string) string {
	parts := make([]string, 0, len(source))
	for _, category := range transition.StyleCategories() {
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
	return fmt.Sprintf("bpmA=%.1f bpmB=%.1f delta=%.4f raw=%.4f factorB=%.1fx keyA=%s keyB=%s confA=%.4f confB=%.4f tier=%d strengthA=%.3f strengthB=%.3f barsA=%d barsB=%d",
		a.BPM, b.BPM, folded, raw, factor,
		analysis.CamelotCode(a.Tonic, a.Minor), analysis.CamelotCode(b.Tonic, b.Minor),
		a.KeyConfidence, b.KeyConfidence, analysis.KeyTier(a, b),
		a.BeatStrength, b.BeatStrength, len(a.BarOffsets), len(b.BarOffsets))
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
	seekSec := cs.bSeekSec
	tempo := cs.bTempo
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

		stream, err := player.startStream(ffmpeg.Args(freshURL, seekSec, normalization, tempo), collectTail)
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
			if cs.trimBLead && !cs.bLoudSeen {
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

func (cs *crossfadeState) nextBFrame() []int16 {
	bFrame := cs.pullBFrame()
	if cs.bLoop != nil {
		return cs.bLoop.Next(bFrame)
	}
	return bFrame
}

func (cs *crossfadeState) mixAndSend(player *GuildPlayer, conn voiceConnection, stopCh chan struct{}, aFrame, bFrame []int16, volume float64, enc *opus.Encoder) error {
	progress := 0.0
	if cs.crossfadeFrames > 0 {
		progress = float64(cs.mixedFrames) / float64(cs.crossfadeFrames)
	}

	copy(cs.mixFloat, cs.processor.Mix(aFrame, bFrame, progress, volume))
	cs.limiter.ProcessStereo(cs.mixFloat, dsp.FullScale*max(1, volume))
	dsp.FloatToFrame(cs.mixFloat, cs.mixBuf)

	opusLen, err := enc.Encode(cs.mixBuf, cs.opusScratch)
	if err != nil {
		cs.scope.Errorf("opus encoding error: %v", err)
		return nil
	}
	opusData := make([]byte, opusLen)
	copy(opusData, cs.opusScratch[:opusLen])

	return sendFrame(conn, opusData, stopCh)
}

func (cs *crossfadeState) handoff(player *GuildPlayer, enc *opus.Encoder) {
	var tail *transition.Tail
	if cs.processor != nil {
		tail = cs.processor.MakeTail()
	}

	player.mu.Lock()
	player.PendingStream = &PendingStream{
		SongID:            cs.nextSongID,
		Stream:            cs.bStream,
		Encoder:           enc,
		FramesConsumed:    cs.bFramesConsumed,
		LeadingSkipFrames: cs.bLeadSkipFrames,
		StartOffsetSec:    cs.bSeekSec + cs.bTempo.Drift(),
		Tail:              tail,
	}
	player.mu.Unlock()
	cs.handedOff = true
	cs.active = false
	cs.scope.Debugf("handed off to song ID %d after %d crossfade frames for guild: %s", cs.nextSongID, cs.mixedFrames, player.GuildID)
}

func (cs *crossfadeState) consume(player *GuildPlayer, conn voiceConnection, stopCh chan struct{}, pcmData []int16, volume float64, enc *opus.Encoder, sentFrames *int) (bool, error) {
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
	if cs.beatLoop != nil {
		aFrame = cs.beatLoop.Next(pcmData)
	}

	bFrame := cs.nextBFrame()
	if err := cs.mixAndSend(player, conn, stopCh, aFrame, bFrame, volume, enc); err != nil {
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

func (cs *crossfadeState) finishOnDrain(player *GuildPlayer, conn voiceConnection, stopCh chan struct{}, enc *opus.Encoder, sentFrames *int) (bool, error) {
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
		if cs.beatLoop != nil && cs.beatLoop.IsReady() {
			aFrame = cs.beatLoop.Next(nil)
		}

		bFrame := cs.nextBFrame()
		if err := cs.mixAndSend(player, conn, stopCh, aFrame, bFrame, volume, enc); err != nil {
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
	cs.bAborted.Store(true)
	if cs.bStream != nil && !cs.handedOff {
		cs.bStream.Stop()
	}
	if pending := cs.bRefetch.Swap(nil); pending != nil {
		pending.stream.Stop()
	}
}
