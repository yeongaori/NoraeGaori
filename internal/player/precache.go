package player

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"os/exec"
	"time"

	"noraegaori/internal/dependency"
	"noraegaori/internal/logger"
	"noraegaori/internal/queue"
)

const (
	preCacheTTL        = time.Hour
	preCacheRetryDelay = 30 * time.Second
	analysisHeadSecs   = 75
	analysisReadMargin = 5
	analysisReadChunk  = 32 * 1024
)

var (
	preCacheNext          = PreCacheNext
	analyzeSegment        = analyzeStreamSegment
	startAnalysisBackfill = StartAnalysisBackfill
)

func PreCacheNext(guildID string, bitrate int) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) < 2 {
		logger.Debugf("No next song to cache for guild: %s", guildID)
		return
	}

	nextSong := q.Songs[1]

	if nextSong.IsLive {
		logger.Debugf("Skipping pre-cache for live stream: %s", nextSong.Title)
		startAnalysisBackfill(guildID, bitrate)
		return
	}

	if nextSong.SeekTime > 0 {
		logger.Debugf("Skipping pre-cache for song with seek time: %s", nextSong.Title)
		startAnalysisBackfill(guildID, bitrate)
		return
	}

	cacheKey := preCacheKey(guildID, nextSong.ID)
	preCacheStoreMu.Lock()
	isRetry := false
	if cached, exists := preCacheStore[cacheKey]; exists && time.Since(cached.Timestamp) < preCacheTTL {
		if cached.FailedAt.IsZero() {
			preCacheStoreMu.Unlock()
			logger.Debugf("Song already cached: %s", nextSong.Title)
			return
		}
		if time.Since(cached.FailedAt) < preCacheRetryDelay {
			preCacheStoreMu.Unlock()
			logger.Debugf("Song recently failed to pre-cache: %s", nextSong.Title)
			return
		}
		isRetry = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	claim := &PreCache{
		SongID:     nextSong.ID,
		Timestamp:  time.Now(),
		CancelFunc: cancel,
	}
	preCacheStore[cacheKey] = claim
	preCacheStoreMu.Unlock()

	if isRetry {
		logger.Debugf("Retrying pre-cache for: %s", nextSong.Title)
	} else {
		logger.Debugf("Starting pre-cache for: %s", nextSong.Title)
	}

	go func() {
		defer cancel()

		if err := preCacheSong(ctx, guildID, nextSong, q.SponsorBlock, bitrate); err != nil {
			logger.Errorf("Failed to pre-cache %s: %v", nextSong.Title, err)
			preCacheStoreMu.Lock()
			if preCacheStore[cacheKey] == claim {
				claim.FailedAt = time.Now()
			}
			preCacheStoreMu.Unlock()
			logger.Debugf("Pre-cache of %s will retry in %.0fs", nextSong.Title, preCacheRetryDelay.Seconds())
		}
		startAnalysisBackfill(guildID, bitrate)
	}()
}

func preCacheSong(ctx context.Context, guildID string, song *queue.Song, sponsorBlock bool, bitrate int) error {

	fetchStart := time.Now()
	streamURL, err := fetchStreamURL(song.URL, sponsorBlock, bitrate)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	cacheKey := preCacheKey(guildID, song.ID)

	preCacheStoreMu.Lock()
	cache, exists := preCacheStore[cacheKey]
	if !exists {
		preCacheStoreMu.Unlock()
		logger.Debugf("Dropped the stream URL for: %s (pre-cache cancelled while fetching)", song.Title)
		return nil
	}
	cache.StreamURL = streamURL
	preCacheStoreMu.Unlock()

	logger.Debugf("Cached stream URL for: %s (fetched in %.1fs)", song.Title, time.Since(fetchStart).Seconds())

	if automix, err := queue.GetAutoMix(guildID); err == nil && automix {
		head := analysis.LoadTrackAnalysis(song.URL, analysis.SegmentHead)
		reused := head != nil
		source := "reused"

		var analyzeErr error
		var analyzeDuration time.Duration
		if head == nil {
			markAnalysisPending(guildID, song.ID)
			analyzeErr = withAnalysisSlot(ctx, func() error {
				analyzeStart := time.Now()
				var err error
				head, err = analyzeSegment(ctx, streamURL, 0, analysisHeadSecs)
				analyzeDuration = time.Since(analyzeStart)
				return err
			})
			clearAnalysisPending(guildID, song.ID)
			source = fmt.Sprintf("analyzed in %.1fs", analyzeDuration.Seconds())
		}

		if analyzeErr == nil && head != nil {
			preCacheStoreMu.Lock()
			if entry, exists := preCacheStore[cacheKey]; exists {
				entry.Analysis = head
			}
			preCacheStoreMu.Unlock()

			if !reused {
				if saveErr := analysis.SaveTrackAnalysis(song.URL, analysis.SegmentHead, head); saveErr != nil {
					logger.Warnf("Failed to save head analysis for %s: %v", song.Title, saveErr)
				}
			}
			logger.Debugf("Analyzed head for: %s (BPM %.1f, key %s / %s, confidence %.3f, %s)",
				song.Title, head.BPM, analysis.KeyName(head.Tonic, head.Minor),
				analysis.FifthsCode(head.Tonic, head.Minor), head.KeyConfidence, source)
		} else {
			logger.Debugf("Head analysis failed for %s after %.1fs: %v", song.Title, analyzeDuration.Seconds(), analyzeErr)
		}
	}

	time.AfterFunc(preCacheTTL, func() {
		preCacheStoreMu.Lock()
		if cached, exists := preCacheStore[cacheKey]; exists && cached.Timestamp.Equal(cache.Timestamp) {
			delete(preCacheStore, cacheKey)
			logger.Debugf("Expired cache for: %s", song.Title)
		}
		preCacheStoreMu.Unlock()
	})

	return nil
}

func preCacheKey(guildID string, songID int) string {
	return fmt.Sprintf("%s_%d", guildID, songID)
}

func lookupPreCacheLocked(guildID string, songID int) *PreCache {
	cached, exists := preCacheStore[preCacheKey(guildID, songID)]
	if !exists {
		return nil
	}

	if time.Since(cached.Timestamp) > preCacheTTL {
		return nil
	}

	return cached
}

func GetPreCache(guildID string, songID int) *PreCache {
	preCacheStoreMu.RLock()
	defer preCacheStoreMu.RUnlock()
	return lookupPreCacheLocked(guildID, songID)
}

func GetCachedStreamURL(guildID string, songID int) string {
	preCacheStoreMu.RLock()
	defer preCacheStoreMu.RUnlock()

	cache := lookupPreCacheLocked(guildID, songID)
	if cache == nil {
		return ""
	}
	return cache.StreamURL
}

func GetCachedAnalysis(guildID string, songID int) *analysis.TrackAnalysis {
	preCacheStoreMu.RLock()
	defer preCacheStoreMu.RUnlock()

	cache := lookupPreCacheLocked(guildID, songID)
	if cache == nil {
		return nil
	}
	return cache.Analysis
}

func readFloat32Samples(reader io.Reader, maxBytes int64) ([]float32, error) {
	samples := make([]float32, 0, maxBytes/4)
	chunk := make([]byte, analysisReadChunk)
	limited := io.LimitReader(reader, maxBytes)

	carry := 0
	for {
		read, err := limited.Read(chunk[carry:])
		total := carry + read
		usable := total - total%4
		for i := 0; i < usable; i += 4 {
			samples = append(samples, math.Float32frombits(binary.LittleEndian.Uint32(chunk[i:])))
		}
		carry = total - usable
		copy(chunk, chunk[usable:total])
		if err != nil {
			if err == io.EOF {
				return samples, nil
			}
			return nil, err
		}
	}
}

func analysisReadBytes(seconds int) int64 {
	return int64(float64(seconds+analysisReadMargin) * analysis.SampleRate * 4)
}

func analyzeStreamSegment(ctx context.Context, streamURL string, startSec float64, seconds int) (*analysis.TrackAnalysis, error) {
	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
	}
	if startSec > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", startSec))
	}
	args = append(args,
		"-t", fmt.Sprintf("%d", seconds),
		"-i", streamURL,
		"-ac", "1",
		"-ar", "24000",
		"-f", "f32le",
		"pipe:1",
	)

	ffmpegBinary := dependency.AcquireFFmpeg()
	defer ffmpegBinary.Release()

	ffmpeg := exec.CommandContext(ctx, ffmpegBinary.Path, args...) // #nosec G204
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	if err := ffmpeg.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	samples, err := readFloat32Samples(stdout, analysisReadBytes(seconds))
	if err != nil {
		if killErr := ffmpeg.Process.Kill(); killErr != nil {
			logger.Debugf("Failed to kill ffmpeg: %v", killErr)
		}
		_ = ffmpeg.Wait()
		return nil, fmt.Errorf("read error: %w", err)
	}

	_, _ = io.Copy(io.Discard, stdout)
	if err := ffmpeg.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w", err)
	}

	lead, trail := dsp.SilentEdges(samples)
	if lead >= len(samples) {
		return nil, fmt.Errorf("segment is entirely silent")
	}
	segment, err := analysis.AnalyzeAfterLead(samples[:len(samples)-trail], lead, analysis.SampleRate)
	if err != nil {
		return nil, err
	}
	segment.Offset = startSec
	return segment, nil
}

func ClearPreCache(guildID string) {
	preCacheStoreMu.Lock()
	defer preCacheStoreMu.Unlock()

	prefix := guildID + "_"
	for key := range preCacheStore {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(preCacheStore, key)
		}
	}

	logger.Debugf("Cleared all caches for guild: %s", guildID)
}

func invalidatePreCacheSong(guildID string, songID int) {
	cacheKey := preCacheKey(guildID, songID)
	preCacheStoreMu.Lock()
	defer preCacheStoreMu.Unlock()

	if cache, exists := preCacheStore[cacheKey]; exists {
		if cache.CancelFunc != nil {
			cache.CancelFunc()
		}
		delete(preCacheStore, cacheKey)
		logger.Debugf("Invalidated cache for song ID %d in guild: %s", songID, guildID)
	}
}

func InvalidatePreCaches(guildID string, songIDs []int) {
	for _, songID := range songIDs {
		invalidatePreCacheSong(guildID, songID)
	}
}
