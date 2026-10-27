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
	armed                bool
	active               bool
	handedOff            bool
	cancelled            bool
	trimSilence          bool
	trimBLead            bool
	bLoudSeen            bool
	autoMix              bool
	scope                *logger.Scoped
	bStream              audioStream
	nextSongID           int
	bSeekSec             float64
	bTempo               ffmpeg.Tempo
	transitionFrame      int
	crossfadeFrames      int
	minUsableFrames      int
	totalFrames          int
	slideFrames          int
	mixedFrames          int
	bFramesConsumed      int
	bLeadSkipFrames      int
	mixBuf               []int16
	mixFloat             []float64
	limiter              dsp.Limiter
	opusScratch          []byte
	processor            *transition.Processor
	beatLoop             *transition.BeatLoop
	bLoop                *transition.BeatLoop
	guildID              string
	normalization        bool
	bitrate              int
	bRetried             bool
	hasLoggedURLWait     bool
	isWaitingForAnalysis bool
	bRefetch             atomic.Pointer[streamRef]
	bRefetching          atomic.Bool
	bAborted             atomic.Bool
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

func findNextCrossfade(guildID string) (*queue.Song, *queue.Song, string) {
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) < 2 {
		return nil, nil, ""
	}
	if q.Songs[0].IsLive || q.Songs[1].IsLive {
		return nil, nil, ""
	}

	return q.Songs[0], q.Songs[1], GetCachedStreamURL(guildID, q.Songs[1].ID)
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
	return float64(youtube.ParseDuration(song.Duration))
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

func (cs *crossfadeState) createPlan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, originSec float64, fade *fadeSettings, normalization bool, bitrate int) *crossfadePlan {
	guildID := player.GuildID
	current, next, nextURL, scope, ok := cs.pickCrossfadeTarget(guildID, fade)
	if !ok {
		return nil
	}
	effectiveEnd := es.TotalFrames - es.SilentTailFrames

	var aAnal, bAnal *analysis.TrackAnalysis
	var placement transitionPlacement
	if fade.autoMix {
		aAnal = es.Analysis
		placement, bAnal, ok = cs.placeTransition(scope, guildID, es, current, next, sentFrames, originSec, fade, effectiveEnd)
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
			describeInputs(aAnal, bAnal), describeStyleSources(resolved.Sources)),
	}
}

func (cs *crossfadeState) pickCrossfadeTarget(guildID string, fade *fadeSettings) (*queue.Song, *queue.Song, string, *logger.Scoped, bool) {
	if (!fade.autoMix && !fade.crossfade) || cs.armed {
		return nil, nil, "", nil, false
	}
	if fade.repeatMode == queue.RepeatSingle {
		return nil, nil, "", nil, false
	}

	current, next, nextURL := findNextCrossfade(guildID)
	if next == nil {
		return nil, nil, "", nil, false
	}

	scope := crossfadeScope(fade.autoMix)
	if nextURL == "" {
		if !cs.hasLoggedURLWait {
			cs.hasLoggedURLWait = true
			scope.Debugf("next song %s has no cached stream URL yet for guild: %s", next.Title, guildID)
		}
		return nil, nil, "", nil, false
	}
	return current, next, nextURL, scope, true
}

func crossfadeScope(autoMix bool) *logger.Scoped {
	if autoMix {
		return logger.Scope("AutoMix")
	}
	return logger.Scope("Crossfade")
}

func (cs *crossfadeState) placeTransition(scope *logger.Scoped, guildID string, es *ffmpeg.EndState, current, next *queue.Song, sentFrames int, originSec float64, fade *fadeSettings, effectiveEnd int) (transitionPlacement, *analysis.TrackAnalysis, bool) {
	bAnal := LookupAnalysis(guildID, next, analysis.SegmentHead)
	pair := &transition.Pair{
		From: transition.Track{
			URL:      current.URL,
			Duration: songSeconds(current),
			End:      originSec + float64(effectiveEnd)/dsp.FramesPerSecond,
			Analysis: es.Analysis,
		},
		To:            transition.Track{URL: next.URL, Duration: songSeconds(next), Analysis: bAnal},
		MaxBeats:      fade.autoMixBeats,
		EarliestStart: originSec + float64(sentFrames)/dsp.FramesPerSecond + transitionLeadSec,
		Settings:      transition.ResolveSettings(songOverrides(current)),
	}
	remainingSec := float64(effectiveEnd-sentFrames) / dsp.FramesPerSecond
	if bAnal == nil && cs.holdForAnalysis(scope, pair, next, fade, remainingSec, guildID) {
		return transitionPlacement{}, bAnal, false
	}
	placement, ok := placeAutoMix(pair, originSec, effectiveEnd)
	return placement, bAnal, ok
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
	p := cs.createPlan(player, es, sentFrames, originSec, fade, normalization, bitrate)
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

func describeInputs(a, b *analysis.TrackAnalysis) string {
	if a == nil || b == nil {
		return fmt.Sprintf("analysis unavailable (outgoing tail %s, next head %s)", describeAnalysis(a), describeAnalysis(b))
	}
	raw := math.Abs(b.BPM-a.BPM) / a.BPM
	folded, factor := analysis.TempoDeltaFactor(a.BPM, b.BPM)
	return fmt.Sprintf("bpmA=%.1f bpmB=%.1f delta=%.4f raw=%.4f factorB=%.1fx keyA=%s keyB=%s confA=%.4f confB=%.4f tier=%d strengthA=%.3f strengthB=%.3f barsA=%d barsB=%d",
		a.BPM, b.BPM, folded, raw, factor,
		analysis.FifthsCode(a.Tonic, a.Minor), analysis.FifthsCode(b.Tonic, b.Minor),
		a.KeyConfidence, b.KeyConfidence, analysis.KeyTier(a, b),
		a.BeatStrength, b.BeatStrength, len(a.BarOffsets), len(b.BarOffsets))
}

func (cs *crossfadeState) holdForAnalysis(scope *logger.Scoped, pair *transition.Pair, next *queue.Song, fade *fadeSettings, remainingSec float64, guildID string) bool {
	if PendingAnalyses(guildID)[next.ID] == 0 {
		return false
	}

	_, outroSec := transition.CrossfadeFrames(fade.autoMix, fade.autoMixBeats, fade.crossfadeSec, pair.From.Analysis)
	deadlineSec := math.Max(pair.LongestOverlapSec(), outroSec) + transitionLeadSec
	if remainingSec > deadlineSec {
		if !cs.isWaitingForAnalysis {
			cs.isWaitingForAnalysis = true
			scope.Debugf("waiting for the head analysis of %s, %.1fs left, planning by %.1fs left for guild: %s", next.Title, remainingSec, deadlineSec, guildID)
		}
		return true
	}

	if cs.isWaitingForAnalysis {
		cs.isWaitingForAnalysis = false
		scope.Debugf("planning without the head analysis of %s, %.1fs left for guild: %s", next.Title, remainingSec, guildID)
	}
	return false
}

func describeAnalysis(a *analysis.TrackAnalysis) string {
	if a == nil {
		return "missing"
	}
	return "present"
}
