package player

import (
	"context"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"sync"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
)

const (
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

	fetchBackfillStreamURL = youtube.GetStreamURLContext
	analysisBackfillGap    = 2 * time.Second
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

type backfillItem struct {
	song    *queue.Song
	segment string
}

func analysisFailureKey(url, segment string) string {
	return url + "\x00" + segment
}

func markAnalysisFailed(url, segment string) {
	if url == "" {
		return
	}
	analysisFailuresMu.Lock()
	defer analysisFailuresMu.Unlock()
	analysisFailures[analysisFailureKey(url, segment)] = time.Now()
}

func analysisFailed(url, segment string) bool {
	if url == "" {
		return false
	}
	analysisFailuresMu.Lock()
	defer analysisFailuresMu.Unlock()

	key := analysisFailureKey(url, segment)
	failedAt, exists := analysisFailures[key]
	if !exists {
		return false
	}
	if time.Since(failedAt) > analysisFailureTTL {
		delete(analysisFailures, key)
		return false
	}
	return true
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
		logger.Debugf("Stopped analysis backfill for guild: %s", guildID)
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
		logger.Debugf("Analyzed %d songs for guild: %s", total, guildID)
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

	items := planBackfillWork(guildID, q.Songs, playerActive(guildID))
	markAnalysisPending(guildID, backfillSongIDs(items)...)
	finished := 0
	defer func() {
		clearAnalysisPending(guildID, backfillSongIDs(items[finished:])...)
	}()

	analyzed := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return analyzed, ctx.Err()
		}
		if TransitionPending(guildID) {
			logger.Debugf("Deferring pass, transition pending for guild: %s", guildID)
			return analyzed, nil
		}

		isAnalyzed := analyzeBackfillItem(ctx, guildID, item, bitrate)
		clearAnalysisPending(guildID, item.song.ID)
		finished++
		if !isAnalyzed {
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

func planBackfillWork(guildID string, songs []*queue.Song, isPlaying bool) []backfillItem {
	var items []backfillItem
	for index, song := range songs {
		if index >= analysisBackfillLimit {
			break
		}
		segment := analysis.SegmentHead
		if index == 0 && isPlaying {
			segment = analysis.SegmentTail
		}
		if needsBackfill(guildID, song, segment) {
			items = append(items, backfillItem{song: song, segment: segment})
		}
	}
	return items
}

func needsBackfill(guildID string, song *queue.Song, segment string) bool {
	if song.IsLive || analysisFailed(song.URL, segment) {
		return false
	}
	if analysis.LoadTrackAnalysis(song.URL, segment) != nil {
		return false
	}
	if segment == analysis.SegmentTail {
		return songSeconds(song) > 0
	}
	return GetPreCache(guildID, song.ID) == nil
}

func backfillSongIDs(items []backfillItem) []int {
	songIDs := make([]int, len(items))
	for index := range items {
		songIDs[index] = items[index].song.ID
	}
	return songIDs
}

func (item *backfillItem) window() (float64, int) {
	if item.segment == analysis.SegmentTail {
		return max(0, songSeconds(item.song)-ffmpeg.TailWindowSeconds), ffmpeg.TailWindowSeconds
	}
	return 0, analysisHeadSecs
}

func analyzeBackfillItem(ctx context.Context, guildID string, item backfillItem, bitrate int) bool {
	song := item.song
	analyzed := false
	if slotErr := withAnalysisSlot(ctx, func() error {
		if ctx.Err() != nil || !needsBackfill(guildID, song, item.segment) {
			return nil
		}

		sponsorBlock := false
		if q, err := queue.GetQueue(guildID, false); err == nil && q != nil {
			sponsorBlock = q.SponsorBlock
		}

		songCtx, cancel := context.WithTimeout(ctx, analysisBackfillTimeout)
		defer cancel()

		streamURL, err := fetchBackfillStreamURL(songCtx, song.URL, sponsorBlock, bitrate)
		if err != nil {
			if ctx.Err() == nil {
				markAnalysisFailed(song.URL, item.segment)
			}
			logger.Debugf("Stream URL failed for %s: %v", song.Title, err)
			return nil
		}

		startSec, seconds := item.window()
		result, err := analyzeSegment(songCtx, streamURL, startSec, seconds)
		if err != nil {
			if ctx.Err() == nil {
				markAnalysisFailed(song.URL, item.segment)
			}
			logger.Debugf("Analysis of the %s failed for %s: %v", item.segment, song.Title, err)
			return nil
		}

		if analysis.LoadTrackAnalysis(song.URL, item.segment) != nil {
			logger.Debugf("Kept the %s analysis stored meanwhile for %s", item.segment, song.Title)
			return nil
		}
		if saveErr := analysis.SaveTrackAnalysis(song.URL, item.segment, result); saveErr != nil {
			logger.Warnf("Failed to save %s analysis for %s: %v", item.segment, song.Title, saveErr)
		}
		logger.Debugf("Analyzed %s for: %s (BPM %.1f, key %s / %s, confidence %.3f)",
			item.segment, song.Title, result.BPM, analysis.KeyName(result.Tonic, result.Minor),
			analysis.FifthsCode(result.Tonic, result.Minor), result.KeyConfidence)
		analyzed = true
		return nil
	}); slotErr != nil {
		logger.Debugf("Backfill slot failed for %s: %v", song.Title, slotErr)
	}
	return analyzed
}
