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
	
	StrategyNone       = 0 
	StrategyFullMemory = 1 
	StrategyStreamURL  = 2 
	StrategyRangeReq   = 3 

	
	maxFullCacheSize  = 15 * 1024 * 1024 
	rangeCacheSize    = 4 * 1024 * 1024  
	preCacheTTL       = 5 * time.Minute  
	currentStrategy   = StrategyStreamURL 
)

func PreCacheNext(guildID string, bitrate int) {
	if currentStrategy == StrategyNone {
		logger.Debugf("[PreCache] Pre-caching disabled")
		return
	}

	
	q, err := queue.GetQueue(guildID, false)
	if err != nil || q == nil || len(q.Songs) < 2 {
		logger.Debugf("[PreCache] No next song to cache for guild: %s", guildID)
		return
	}

	nextSong := q.Songs[1]

	
	if nextSong.IsLive {
		logger.Debugf("[PreCache] Skipping pre-cache for live stream: %s", nextSong.Title)
		return
	}

	if nextSong.SeekTime > 0 {
		logger.Debugf("[PreCache] Skipping pre-cache for song with seek time: %s", nextSong.Title)
		return
	}

	
	cacheKey := fmt.Sprintf("%s_%d", guildID, nextSong.ID)
	preCacheStoreMu.RLock()
	cached, exists := preCacheStore[cacheKey]
	preCacheStoreMu.RUnlock()

	if exists && time.Since(cached.Timestamp) < preCacheTTL {
		logger.Debugf("[PreCache] Song already cached: %s", nextSong.Title)
		return
	}

	logger.Debugf("[PreCache] Starting pre-cache for: %s", nextSong.Title)

	
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
			logger.Errorf("[PreCache] Failed to pre-cache %s: %v", nextSong.Title, err)
		}
	}()
}

func preCacheSong(ctx context.Context, guildID string, song *queue.Song, sponsorBlock bool, bitrate int) error {
	
	streamURL, err := youtube.GetStreamURL(song.URL, sponsorBlock, bitrate)
	if err != nil {
		return fmt.Errorf("failed to get stream URL: %w", err)
	}

	cacheKey := fmt.Sprintf("%s_%d", guildID, song.ID)

	
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

		logger.Debugf("[PreCache] Cached stream URL for: %s", song.Title)

		
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

	logger.Debugf("[PreCache] Cached %d KB + stream URL for: %s", len(buffer)/1024, song.Title)

	
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
			logger.Debugf("[PreCache] Cancelled pre-cache worker for: %s (song ID: %d)", nextSong.Title, nextSong.ID)
		}
		delete(preCacheStore, cacheKey)
		logger.Debugf("[PreCache] Cleaned up pre-cache for guild: %s", guildID)
	}
}
