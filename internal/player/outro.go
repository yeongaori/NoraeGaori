package player

import (
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
)

const outroMaxTailFrames = 200

type outroState struct {
	armed       bool
	committed   bool
	startFrame  int
	outroFrames int
	appliedNext int
	recipe      transition.Recipe
	processor   *transition.Processor
	tail        *transition.Tail
	tailBuf     []int16
	opusScratch []byte
}

func newOutroState() *outroState {
	return &outroState{recipe: transition.DefaultRecipe()}
}

func planOutroWindow(es *ffmpeg.EndState, sentFrames int, fade fadeSettings) (int, int, bool) {
	outroFrames, _ := transition.CrossfadeFrames(fade.autoMix, fade.autoMixBeats, fade.crossfadeSec, es.Analysis)
	if outroFrames < minUsableCrossfadeFrames {
		return 0, 0, false
	}

	effectiveEnd := es.TotalFrames - es.SilentTailFrames
	startFrame := effectiveEnd - outroFrames
	if startFrame < sentFrames+1 {
		return 0, 0, false
	}
	return startFrame, outroFrames, true
}

func (os *outroState) plan(player *GuildPlayer, es *ffmpeg.EndState, sentFrames int, fade fadeSettings) bool {
	if os.armed || os.committed {
		return false
	}
	if !fade.autoMix || fade.repeatMode == queue.RepeatSingle {
		return false
	}

	trackAnalysis := es.Analysis
	startFrame, outroFrames, ok := planOutroWindow(es, sentFrames, fade)
	if !ok {
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

	effectiveEnd := es.TotalFrames - es.SilentTailFrames

	songOverrides := songTransitionOverrides(q.Songs[0])
	recipe, _, styleSource := transition.ResolveOutroStyles(trackAnalysis, fade.autoMix, fade.styleOverrides, songOverrides)

	periodSec := 0.0
	if trackAnalysis != nil {
		periodSec = trackAnalysis.PeriodSec
	}

	loopStyle, _ := transition.ClampLoopStyle(recipe.Loop, periodSec, outroFrames)
	recipe.Loop = loopStyle

	if trackAnalysis != nil {
		if transition.LoopBeatCount(recipe.Loop) >= analysis.BarBeats {
			startFrame = snapTransitionToBar(startFrame, es.TailStartFrame, trackAnalysis)
		} else {
			startFrame = snapTransitionToGrid(startFrame, es.TailStartFrame, trackAnalysis)
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
	os.processor = transition.NewProcessor(recipe, os.outroFrames, periodSec)
	os.appliedNext = 0

	logger.Debugf("planned at frame %d (%d frames) for guild: %s", startFrame, os.outroFrames, guildID)
	logger.Debugf("recipe %s (%s) [%s] for guild: %s", recipe,
		describeOutroInput(trackAnalysis), describeStyleSources(styleSource), guildID)
	return true
}

func describeOutroInput(a *analysis.TrackAnalysis) string {
	if a == nil {
		return "analysis unavailable"
	}
	return fmt.Sprintf("bpm=%.1f key=%s period=%.4f", a.BPM, analysis.CamelotCode(a.Tonic, a.Minor), a.PeriodSec)
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
		logger.Debugf("started at frame %d", sentFrames)
	}

	progress := 0.0
	if os.outroFrames > 0 {
		progress = float64(os.appliedNext) / float64(os.outroFrames)
	}
	if progress > 1 {
		progress = 1
	}

	buf := os.processor.ProcessA(frame, progress)
	os.processor.ApplyGainA(buf, progress, volume)
	dsp.FloatToFrame(buf, frame)
	os.appliedNext++
}

func (os *outroState) flush(player *GuildPlayer, stopCh chan struct{}, enc *opus.Encoder, sentFrames *int) {
	if !os.committed || os.processor == nil {
		return
	}
	if os.tail == nil {
		os.tail = os.processor.MakeTail(os.processor.LastGain())
	}
	if os.tail == nil {
		return
	}
	if os.tailBuf == nil {
		os.tailBuf = make([]int16, frameSize*channels)
	}
	if os.opusScratch == nil {
		os.opusScratch = make([]byte, maxOpusFrameBytes)
	}

	emitted := 0
	for emitted < outroMaxTailFrames {
		for i := range os.tailBuf {
			os.tailBuf[i] = 0
		}
		more := os.tail.Apply(os.tailBuf)

		opusLen, err := enc.Encode(os.tailBuf, os.opusScratch)
		if err != nil {
			logger.Errorf("opus encoding error: %v", err)
			return
		}

		packet := make([]byte, opusLen)
		copy(packet, os.opusScratch[:opusLen])

		select {
		case player.VoiceConn.OpusSendChan() <- packet:
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
		logger.Debugf("flushed %d tail frames for guild: %s", emitted, player.GuildID)
	}
}
