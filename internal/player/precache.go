package player

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

const (
	// Pre-cache strategies
	StrategyNone       = 0 // No pre-caching
	StrategyFullMemory = 1 // Cache entire song (max 15MB)
	StrategyStreamURL  = 2 // Cache stream URL only (lightweight)
	StrategyRangeReq   = 3 // Cache first 4MB

	// Limits
	maxFullCacheSize  = 15 * 1024 * 1024 // 15MB
	rangeCacheSize    = 4 * 1024 * 1024  // 4MB
	preCacheTTL       = 5 * time.Minute  // Cache expiration
	currentStrategy   = StrategyStreamURL // Default: pre-cache stream URL
)

// PreCacheNext pre-caches the next song in queue
func PreCacheNext(guildID string, bitrate int) {
	if currentStrategy == StrategyNone {
		logger.Debugf("[PreCache] Pre-caching disabled")
		return
	}

	// Get queue
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) < 2 {
		logger.Debugf("[PreCache] No next song to cache for guild: %s", guildID)
		return
	}

	nextSong := q.Songs[1]

	// Skip live streams and songs with seek time
	if nextSong.IsLive {
		logger.Infof("[PreCache] Skipping pre-cache for live stream: %s", nextSong.Title)
		return
	}

	if nextSong.SeekTime > 0 {
		logger.Debugf("[PreCache] Skipping pre-cache for song with seek time: %s", nextSong.Title)
		return
	}

	// Check if already cached
	cacheKey := fmt.Sprintf("%s_%d", guildID, nextSong.ID)
	preCacheStoreMu.RLock()
	cached, exists := preCacheStore[cacheKey]
	preCacheStoreMu.RUnlock()

	if exists && time.Since(cached.Timestamp) < preCacheTTL {
		logger.Debugf("[PreCache] Song already cached: %s", nextSong.Title)
		return
	}

	logger.Infof("[PreCache] Starting pre-cache for: %s", nextSong.Title)

	// Run pre-cache in goroutine
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Store cancel function for cleanup
		cacheKey := fmt.Sprintf("%s_%d", guildID, nextSong.ID)
		preCacheStoreMu.Lock()
		preCacheStore[cacheKey] = &PreCache{
			SongID:     nextSong.ID,
			Timestamp:  time.Now(),
			CancelFunc: cancel,
		}
		preCacheStoreMu.Unlock()

		if err := preCacheSong(ctx, guildID, nextSong, q.SponsorBlock, bitrate); err != nil {
			logger.Errorf("[PreCache] Failed to pre-cache %s: %v", nextSong.Title, err)
		}
	}()
}

// preCacheSong pre-caches a song based on strategy
func preCacheSong(ctx context.Context, guildID string, song *queue.Song, sponsorBlock bool, bitrate int) error {
	// Get stream URL using the same bitrate as the main play path
	streamURL, err := youtube.GetStreamURL(song.URL, sponsorBlock, bitrate)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	cacheKey := fmt.Sprintf("%s_%d", guildID, song.ID)

	// For stream URL strategy, just store the URL and return
	if currentStrategy == StrategyStreamURL {
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

		logger.Infof("[PreCache] Cached stream URL for: %s", song.Title)

		// Set expiration
		go func() {
			time.Sleep(preCacheTTL)
			preCacheStoreMu.Lock()
			if cached, exists := preCacheStore[cacheKey]; exists && cached.Timestamp.Equal(cache.Timestamp) {
				delete(preCacheStore, cacheKey)
				logger.Debugf("[PreCache] Expired cache for: %s", song.Title)
			}
			preCacheStoreMu.Unlock()
		}()
		return nil
	}

	// For PCM-based strategies, run FFmpeg and cache audio data
	args := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", streamURL,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-t", "30",
		"pipe:1",
	}

	ffmpeg := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := ffmpeg.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	var buffer []byte
	maxSize := rangeCacheSize
	if currentStrategy == StrategyFullMemory {
		maxSize = maxFullCacheSize
	}

	buf := make([]byte, 4096)
	totalRead := 0

	for totalRead < maxSize {
		select {
		case <-ctx.Done():
			ffmpeg.Process.Kill()
			return ctx.Err()
		default:
			n, err := stdout.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				ffmpeg.Process.Kill()
				return fmt.Errorf("read error: %w", err)
			}

			buffer = append(buffer, buf[:n]...)
			totalRead += n

			if currentStrategy == StrategyRangeReq && totalRead >= rangeCacheSize {
				break
			}
		}
	}

	ffmpeg.Process.Kill()

	preCacheStoreMu.Lock()
	if cache, exists := preCacheStore[cacheKey]; exists {
		cache.Data = buffer
		cache.StreamURL = streamURL
	} else {
		preCacheStore[cacheKey] = &PreCache{
			Data:      buffer,
			StreamURL: streamURL,
			SongID:    song.ID,
			Timestamp: time.Now(),
		}
	}
	cache := preCacheStore[cacheKey]
	preCacheStoreMu.Unlock()

	logger.Infof("[PreCache] Cached %d KB + stream URL for: %s", len(buffer)/1024, song.Title)

	// Set expiration
	go func() {
		time.Sleep(preCacheTTL)
		preCacheStoreMu.Lock()
		if cached, exists := preCacheStore[cacheKey]; exists && cached.Timestamp.Equal(cache.Timestamp) {
			delete(preCacheStore, cacheKey)
			logger.Debugf("[PreCache] Expired cache for: %s", song.Title)
		}
		preCacheStoreMu.Unlock()
	}()

	return nil
}

// GetPreCache retrieves pre-cached data for a song
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

// GetCachedStreamURL retrieves a pre-cached stream URL for a song (returns "" if not cached or expired)
func GetCachedStreamURL(guildID string, songID int) string {
	cache := GetPreCache(guildID, songID)
	if cache == nil || cache.StreamURL == "" {
		return ""
	}
	return cache.StreamURL
}

// ClearPreCache clears all pre-cached data for a guild
func ClearPreCache(guildID string) {
	preCacheStoreMu.Lock()
	defer preCacheStoreMu.Unlock()

	prefix := guildID + "_"
	for key := range preCacheStore {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(preCacheStore, key)
		}
	}

	logger.Debugf("[PreCache] Cleared all caches for guild: %s", guildID)
}

// CleanupPreCacheWorker cancels and cleans up the pre-cache worker for next song
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
			cache.CancelFunc() // Cancel the ongoing download
			logger.Debugf("[PreCache] Cancelled pre-cache worker for: %s (song ID: %d)", nextSong.Title, nextSong.ID)
		}
		delete(preCacheStore, cacheKey)
		logger.Debugf("[PreCache] Cleaned up pre-cache for guild: %s", guildID)
	}
}
