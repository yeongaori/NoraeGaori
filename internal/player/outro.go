package player

import (
	"fmt"

	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

const outroMaxTailFrames = 200

type outroState struct {
	armed       bool
	committed   bool
	startFrame  int
	outroFrames int
	appliedNext int
	recipe      TransitionRecipe
	processor   *transitionProcessor
	tail        *transitionTail
	tailBuf     []int16
}

func newOutroState() *outroState {
	return &outroState{recipe: defaultTransitionRecipe()}
}

func planOutroWindow(es *streamEndState, sentFrames int, fade fadeSettings) (int, int, bool) {
	outroFrames, _ := TransitionCrossfadeFrames(fade.autoMix, fade.autoMixBeats, fade.crossfadeSec, es.analysis)
	if outroFrames < minUsableCrossfadeFrames {
		return 0, 0, false
	}

	effectiveEnd := es.totalFrames - es.silentTailFrames
	startFrame := effectiveEnd - outroFrames
	if startFrame < sentFrames+1 {
		return 0, 0, false
	}
	return startFrame, outroFrames, true
}

func (os *outroState) plan(player *GuildPlayer, es *streamEndState, sentFrames int, fade fadeSettings) bool {
	if os.armed || os.committed {
		return false
	}
	if !fade.autoMix || fade.repeatMode == queue.RepeatSingle {
		return false
	}

	guildID := player.GuildID
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		return false
	}
	if len(q.Songs) > 1 {
		return false
	}
	if q.Songs[0].IsLive {
		return false
	}

	analysis := es.analysis
	startFrame, outroFrames, ok := planOutroWindow(es, sentFrames, fade)
	if !ok {
		return false
	}
	effectiveEnd := es.totalFrames - es.silentTailFrames

	songOverrides := songTransitionOverrides(q.Songs[0])
	recipe, _, styleSource := ResolveOutroStyles(analysis, fade.autoMix, fade.styleOverrides, songOverrides)

	periodSec := 0.0
	if analysis != nil {
		periodSec = analysis.PeriodSec
	}

	loopStyle, _ := ClampLoopStyle(recipe.Loop, periodSec, outroFrames)
	recipe.Loop = loopStyle

	if analysis != nil {
		if loopBeatCount(recipe.Loop) >= keyBarBeats {
			startFrame = snapTransitionToBar(startFrame, es.tailStartFrame, analysis)
		} else {
			startFrame = snapTransitionToGrid(startFrame, es.tailStartFrame, analysis)
		}
	}
	if startFrame < sentFrames+1 {
		startFrame = sentFrames + 1
	}
	if startFrame >= effectiveEnd {
		return false
	}

	os.armed = true
	os.startFrame = startFrame
	os.outroFrames = effectiveEnd - startFrame
	os.recipe = recipe
	os.processor = newTransitionProcessor(recipe, os.outroFrames, periodSec)
	os.appliedNext = 0

	logger.Debugf("[Outro] planned at frame %d (%d frames) for guild: %s", startFrame, os.outroFrames, guildID)
	logger.Debugf("[Outro] recipe %s (%s) [%s] for guild: %s", recipe,
		describeOutroInput(analysis), describeStyleSources(styleSource), guildID)
	return true
}

func describeOutroInput(a *TrackAnalysis) string {
	if a == nil {
		return "analysis unavailable"
	}
	return fmt.Sprintf("bpm=%.1f key=%s period=%.4f", a.BPM, camelotCode(a.Tonic, a.Minor), a.PeriodSec)
}

func (os *outroState) cancel() {
	if os.committed {
		return
	}
	if os.armed {
		os.armed = false
		os.processor = nil
		os.appliedNext = 0
	}
}

func (os *outroState) running(sentFrames int) bool {
	return os.armed && os.processor != nil && sentFrames >= os.startFrame
}

func (os *outroState) process(frame []int16, sentFrames int, volume float64) {
	if !os.running(sentFrames) {
		return
	}
	if !os.committed {
		os.committed = true
		logger.Debugf("[Outro] started at frame %d", sentFrames)
	}

	progress := 0.0
	if os.outroFrames > 0 {
		progress = float64(os.appliedNext) / float64(os.outroFrames)
	}
	if progress > 1 {
		progress = 1
	}

	buf := os.processor.processA(frame, progress)
	gain, _ := os.processor.gains(progress)
	gain *= volume

	if !os.processor.gainInitialized {
		os.processor.previousAGain = gain
		os.processor.gainInitialized = true
	}
	applyGainRamp(buf, os.processor.previousAGain, gain)
	os.processor.previousAGain = gain

	floatToFrame(buf, frame)
	os.appliedNext++
}

func (os *outroState) flush(player *GuildPlayer, stopCh chan struct{}, enc *OpusEncoder, sentFrames *int) {
	if !os.committed || os.processor == nil {
		return
	}
	if os.tail == nil {
		os.tail = os.processor.makeTail(os.processor.previousAGain)
	}
	if os.tail == nil {
		return
	}
	if os.tailBuf == nil {
		os.tailBuf = make([]int16, frameSize*channels)
	}

	emitted := 0
	for emitted < outroMaxTailFrames {
		for i := range os.tailBuf {
			os.tailBuf[i] = 0
		}
		more := os.tail.apply(os.tailBuf)

		opusBuffer := make([]byte, 1500)
		opusLen, err := enc.Encode(os.tailBuf, opusBuffer)
		if err != nil {
			logger.Errorf("[Outro] opus encoding error: %v", err)
			return
		}

		select {
		case player.VoiceConn.OpusSendChan() <- opusBuffer[:opusLen]:
		case <-player.VoiceConn.DeadChan():
			return
		case <-stopCh:
			return
		}

		*sentFrames++
		emitted++
		if !more {
			break
		}
	}

	if emitted > 0 {
		logger.Debugf("[Outro] flushed %d tail frames for guild: %s", emitted, player.GuildID)
	}
}
