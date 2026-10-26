package player

import (
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"

	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

func (cs *crossfadeState) bReady() bool {
	return len(cs.bStream.PCM()) > 0 || cs.bStream.EndState() != nil
}

func (cs *crossfadeState) bFailed() bool {
	select {
	case err := <-cs.bStream.Errs():
		cs.scope.Debugf("next stream failed for guild %s: %v", cs.guildID, err)
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
