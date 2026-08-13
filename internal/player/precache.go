package player

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

const (
	preCacheTTL        = time.Hour
	analysisHeadSecs   = 75
	analysisReadMargin = 5
	analysisMaxBytes   = int64((analysisHeadSecs + analysisReadMargin) * tailSampleRate * 4)
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
		return
	}

	if nextSong.SeekTime > 0 {
		logger.Debugf("Skipping pre-cache for song with seek time: %s", nextSong.Title)
		return
	}

	
	cacheKey := fmt.Sprintf("%s_%d", guildID, nextSong.ID)
	preCacheStoreMu.RLock()
	cached, exists := preCacheStore[cacheKey]
	preCacheStoreMu.RUnlock()

	if exists && time.Since(cached.Timestamp) < preCacheTTL {
		logger.Debugf("Song already cached: %s", nextSong.Title)
		return
	}

	logger.Debugf("Starting pre-cache for: %s", nextSong.Title)

	
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		
		cacheKey := fmt.Sprintf("%s_%d", guildID, nextSong.ID)
		preCacheStoreMu.Lock()
		preCacheStore[cacheKey] = &PreCache{
			SongID:     nextSong.ID,
			Timestamp:  time.Now(),
			CancelFunc: cancel,
		}
		preCacheStoreMu.Unlock()

		if err := preCacheSong(ctx, guildID, nextSong, q.SponsorBlock, bitrate); err != nil {
			logger.Errorf("Failed to pre-cache %s: %v", nextSong.Title, err)
		}
		StartAnalysisBackfill(guildID, bitrate)
	}()
}

func preCacheSong(ctx context.Context, guildID string, song *queue.Song, sponsorBlock bool, bitrate int) error {
	
	streamURL, err := youtube.GetStreamURL(song.URL, sponsorBlock, bitrate)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	cacheKey := fmt.Sprintf("%s_%d", guildID, song.ID)

	preCacheStoreMu.Lock()
	if cache, exists := preCacheStore[cacheKey]; exists {
		cache.StreamURL = streamURL
	} else {
		preCacheStore[cacheKey] = &PreCache{
			StreamURL: streamURL,
			SongID:    song.ID,
			Timestamp: time.Now(),
		}
	}
	cache := preCacheStore[cacheKey]
	preCacheStoreMu.Unlock()

	logger.Debugf("Cached stream URL for: %s", song.Title)

	if automix, err := queue.GetAutoMix(guildID); err == nil && automix {
		analysis := LoadTrackAnalysis(song.URL, AnalysisSegmentHead)
		reused := analysis != nil

		var analyzeErr error
		if analysis == nil {
			analyzeErr = withAnalysisSlot(ctx, func() error {
				var err error
				analysis, err = analyzeStreamHead(ctx, streamURL)
				return err
			})
		}

		if analyzeErr == nil && analysis != nil {
			preCacheStoreMu.Lock()
			if entry, exists := preCacheStore[cacheKey]; exists {
				entry.Analysis = analysis
			}
			preCacheStoreMu.Unlock()

			if !reused {
				SaveTrackAnalysis(song.URL, AnalysisSegmentHead, analysis)
			}
			logger.Debugf("Analyzed head for: %s (BPM %.1f, key %s / %s, confidence %.3f, reused %v)",
				song.Title, analysis.BPM, keyName(analysis.Tonic, analysis.Minor),
				camelotCode(analysis.Tonic, analysis.Minor), analysis.KeyConfidence, reused)
		} else {
			logger.Debugf("Head analysis failed for %s: %v", song.Title, analyzeErr)
		}
	}

	go func() {
		time.Sleep(preCacheTTL)
		preCacheStoreMu.Lock()
		if cached, exists := preCacheStore[cacheKey]; exists && cached.Timestamp.Equal(cache.Timestamp) {
			delete(preCacheStore, cacheKey)
			logger.Debugf("Expired cache for: %s", song.Title)
		}
		preCacheStoreMu.Unlock()
	}()

	return nil
}

func GetPreCache(guildID string, songID int) *PreCache {
	cacheKey := fmt.Sprintf("%s_%d", guildID, songID)
	preCacheStoreMu.RLock()
	defer preCacheStoreMu.RUnlock()

	cached, exists := preCacheStore[cacheKey]
	if !exists {
		return nil
	}

	if time.Since(cached.Timestamp) > preCacheTTL {
		return nil
	}

	return cached
}

func GetCachedStreamURL(guildID string, songID int) string {
	cache := GetPreCache(guildID, songID)
	if cache == nil || cache.StreamURL == "" {
		return ""
	}
	return cache.StreamURL
}

func GetCachedAnalysis(guildID string, songID int) *TrackAnalysis {
	cache := GetPreCache(guildID, songID)
	if cache == nil {
		return nil
	}
	return cache.Analysis
}

func analyzeStreamHead(ctx context.Context, streamURL string) (*TrackAnalysis, error) {
	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-t", fmt.Sprintf("%d", analysisHeadSecs),
		"-i", streamURL,
		"-ac", "1",
		"-ar", "24000",
		"-f", "f32le",
		"pipe:1",
	}

	ffmpeg := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	if err := ffmpeg.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	data, err := io.ReadAll(io.LimitReader(stdout, analysisMaxBytes))
	if err != nil {
		ffmpeg.Process.Kill()
		return nil, fmt.Errorf("read error: %w", err)
	}
	io.Copy(io.Discard, stdout)
	if err := ffmpeg.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w", err)
	}

	n := len(data) / 4
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		samples[i] = math.Float32frombits(bits)
	}

	lead := leadingSilentSamples(samples)
	if lead >= len(samples) {
		return nil, fmt.Errorf("head is entirely silent")
	}
	analysis, err := analyzeTrackSamples(samples[lead:], tailSampleRate)
	if err != nil {
		return nil, err
	}
	analysis.FirstBeat += float64(lead) / tailSampleRate
	return analysis, nil
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
	cacheKey := fmt.Sprintf("%s_%d", guildID, songID)
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

func CleanupPreCacheWorker(guildID string) {
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) < 2 {
		return
	}

	nextSong := q.Songs[1]
	cacheKey := fmt.Sprintf("%s_%d", guildID, nextSong.ID)

	preCacheStoreMu.Lock()
	defer preCacheStoreMu.Unlock()

	if cache, exists := preCacheStore[cacheKey]; exists {
		if cache.CancelFunc != nil {
			cache.CancelFunc() 
			logger.Debugf("Cancelled pre-cache worker for: %s (song ID: %d)", nextSong.Title, nextSong.ID)
		}
		delete(preCacheStore, cacheKey)
		logger.Debugf("Cleaned up pre-cache for guild: %s", guildID)
	}
}
