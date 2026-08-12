package player

import (
	"context"
	"sync"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

const (
	analysisBackfillGap       = 2 * time.Second
	analysisBackfillPassGap   = 5 * time.Second
	analysisBackfillLimit     = 50
	analysisBackfillTimeout   = 3 * time.Minute
	analysisBackfillSlotCount = 2
	analysisFailureTTL        = 30 * time.Minute
)

type backfillWorker struct {
	generation int64
	cancel     context.CancelFunc
}

var (
	analysisSlots      = make(chan struct{}, analysisBackfillSlotCount)
	backfillWorkers    = make(map[string]*backfillWorker)
	backfillGeneration int64
	backfillWorkersMu  sync.Mutex

	analysisFailures   = make(map[string]time.Time)
	analysisFailuresMu sync.Mutex
)

func withAnalysisSlot(ctx context.Context, fn func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case analysisSlots <- struct{}{}:
	}
	defer func() { <-analysisSlots }()
	return fn()
}

func markAnalysisFailed(url string) {
	if url == "" {
		return
	}
	analysisFailuresMu.Lock()
	defer analysisFailuresMu.Unlock()
	analysisFailures[url] = time.Now()
}

func AnalysisFailed(url string) bool {
	if url == "" {
		return false
	}
	analysisFailuresMu.Lock()
	defer analysisFailuresMu.Unlock()

	failedAt, exists := analysisFailures[url]
	if !exists {
		return false
	}
	if time.Since(failedAt) > analysisFailureTTL {
		delete(analysisFailures, url)
		return false
	}
	return true
}

func AnalysisBackfillActive(guildID string) bool {
	backfillWorkersMu.Lock()
	defer backfillWorkersMu.Unlock()
	_, running := backfillWorkers[guildID]
	return running
}

func playerActive(guildID string) bool {
	playersMu.Lock()
	defer playersMu.Unlock()
	_, exists := players[guildID]
	return exists
}

func StartAnalysisBackfill(guildID string, bitrate int) {
	if automix, err := queue.GetAutoMix(guildID); err != nil || !automix {
		return
	}

	backfillWorkersMu.Lock()
	if _, running := backfillWorkers[guildID]; running {
		backfillWorkersMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	backfillGeneration++
	worker := &backfillWorker{generation: backfillGeneration, cancel: cancel}
	backfillWorkers[guildID] = worker
	backfillWorkersMu.Unlock()

	go func() {
		defer func() {
			backfillWorkersMu.Lock()
			if current, exists := backfillWorkers[guildID]; exists && current.generation == worker.generation {
				delete(backfillWorkers, guildID)
			}
			backfillWorkersMu.Unlock()
			cancel()
		}()
		runAnalysisBackfill(ctx, guildID, bitrate)
	}()
}

func StopAnalysisBackfill(guildID string) {
	backfillWorkersMu.Lock()
	worker, running := backfillWorkers[guildID]
	if running {
		delete(backfillWorkers, guildID)
	}
	backfillWorkersMu.Unlock()

	if running {
		worker.cancel()
		logger.Debugf("[Backfill] Stopped analysis backfill for guild: %s", guildID)
	}
}

func runAnalysisBackfill(ctx context.Context, guildID string, bitrate int) {
	total := 0
	for {
		analyzed, err := runAnalysisBackfillPass(ctx, guildID, bitrate)
		if err != nil {
			break
		}
		total += analyzed
		if analyzed == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(analysisBackfillPassGap):
		}
	}

	if total > 0 {
		logger.Debugf("[Backfill] Analyzed %d songs for guild: %s", total, guildID)
	}
}

func runAnalysisBackfillPass(ctx context.Context, guildID string, bitrate int) (int, error) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil {
		return 0, err
	}
	if q == nil {
		return 0, context.Canceled
	}

	skipPlaying := playerActive(guildID)

	analyzed := 0
	for index, song := range q.Songs {
		if index >= analysisBackfillLimit {
			break
		}
		if ctx.Err() != nil {
			return analyzed, ctx.Err()
		}
		if index == 0 && skipPlaying {
			continue
		}
		if TransitionPending(guildID) {
			logger.Debugf("[Backfill] Deferring pass, transition pending for guild: %s", guildID)
			return analyzed, nil
		}
		if song.IsLive {
			continue
		}
		if AnalysisFailed(song.URL) {
			continue
		}
		if LoadTrackAnalysis(song.URL, AnalysisSegmentHead) != nil {
			continue
		}
		if GetPreCache(guildID, song.ID) != nil {
			continue
		}

		if !analyzeBackfillSong(ctx, guildID, song, bitrate) {
			if ctx.Err() != nil {
				return analyzed, ctx.Err()
			}
			continue
		}
		analyzed++

		select {
		case <-ctx.Done():
			return analyzed, ctx.Err()
		case <-time.After(analysisBackfillGap):
		}
	}

	return analyzed, nil
}

func analyzeBackfillSong(ctx context.Context, guildID string, song *queue.Song, bitrate int) bool {
	analyzed := false
	withAnalysisSlot(ctx, func() error {
		if ctx.Err() != nil {
			return nil
		}
		if LoadTrackAnalysis(song.URL, AnalysisSegmentHead) != nil {
			return nil
		}

		sponsorBlock := false
		if q, err := queue.GetQueue(guildID, false); err == nil && q != nil {
			sponsorBlock = q.SponsorBlock
		}

		songCtx, cancel := context.WithTimeout(ctx, analysisBackfillTimeout)
		defer cancel()

		streamURL, err := youtube.GetStreamURLContext(songCtx, song.URL, sponsorBlock, bitrate)
		if err != nil {
			if ctx.Err() == nil {
				markAnalysisFailed(song.URL)
			}
			logger.Debugf("[Backfill] Stream URL failed for %s: %v", song.Title, err)
			return nil
		}

		analysis, err := analyzeStreamHead(songCtx, streamURL)
		if err != nil {
			if ctx.Err() == nil {
				markAnalysisFailed(song.URL)
			}
			logger.Debugf("[Backfill] Head analysis failed for %s: %v", song.Title, err)
			return nil
		}

		SaveTrackAnalysis(song.URL, AnalysisSegmentHead, analysis)
		logger.Debugf("[Backfill] Analyzed head for: %s (BPM %.1f, key %s / %s, confidence %.3f)",
			song.Title, analysis.BPM, keyName(analysis.Tonic, analysis.Minor),
			camelotCode(analysis.Tonic, analysis.Minor), analysis.KeyConfidence)
		analyzed = true
		return nil
	})
	return analyzed
}
