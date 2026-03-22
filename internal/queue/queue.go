package queue

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/database"
	"noraegaori/pkg/logger"
)

// SongState represents the current state of a song
type SongState int

const (
	SongStateQueued SongState = iota // Song is waiting in queue
	SongStateLoading                 // Song is being loaded
	SongStatePlaying                 // Song is currently playing
	SongStatePaused                  // Song is paused
	SongStateFailed                  // Song failed to play
	SongStateCompleted               // Song finished successfully
)

// Repeat modes
const (
	RepeatOff    = 0 // No repeat
	RepeatAll    = 1 // Repeat entire queue
	RepeatSingle = 2 // Repeat current song
)

// Song represents a song with all integrated state management
type Song struct {
	// Basic metadata (stored in database)
	ID             int
	GuildID        string
	URL            string
	Title          string
	Duration       string
	Thumbnail      string
	RequestedByID  string
	RequestedByTag string
	QueuePosition  int
	SeekTime       int    // Playback position in milliseconds
	Uploader       string
	IsLive         bool

	// Runtime state (not persisted to database)
	State           SongState              // Current playback state
	RetryCount      int                    // Number of retry attempts
	LastError       error                  // Last error encountered
	LoadingMessage  *discordgo.Message     // Loading message for this song
	PreCacheData    []byte                 // Pre-cached audio data
	PreCacheCancel  context.CancelFunc     // Cancel function for pre-cache
	PlaybackStarted time.Time              // When playback started
	AddedAt         time.Time              // When added to queue
	StateChangedAt  time.Time              // Last state change timestamp
	mu              sync.RWMutex           // Mutex for thread-safe state access
}

// SetState safely updates the song state
func (s *Song) SetState(state SongState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
	s.StateChangedAt = time.Now()
	logger.Debugf("[Song] %s state changed to %s", s.Title, s.getStateName())
}

// GetState safely retrieves the song state
func (s *Song) GetState() SongState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// IncrementRetry safely increments retry count and returns new count
func (s *Song) IncrementRetry() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RetryCount++
	return s.RetryCount
}

// GetRetryCount safely retrieves retry count
func (s *Song) GetRetryCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RetryCount
}

// ResetRetry safely resets retry count to 0
func (s *Song) ResetRetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RetryCount = 0
}

// SetError safely sets the last error
func (s *Song) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastError = err
	if err != nil {
		logger.Errorf("[Song] %s encountered error: %v", s.Title, err)
	}
}

// GetError safely retrieves the last error
func (s *Song) GetError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastError
}

// SetLoadingMessage safely sets the loading message
func (s *Song) SetLoadingMessage(msg *discordgo.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LoadingMessage = msg
}

// GetLoadingMessage safely retrieves the loading message
func (s *Song) GetLoadingMessage() *discordgo.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LoadingMessage
}

// ClearLoadingMessage safely clears the loading message
func (s *Song) ClearLoadingMessage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LoadingMessage = nil
}

// SetPreCache safely sets pre-cached data
func (s *Song) SetPreCache(data []byte, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PreCacheData = data
	s.PreCacheCancel = cancel
	logger.Debugf("[Song] Pre-cache set for %s (%d bytes)", s.Title, len(data))
}

// GetPreCache safely retrieves pre-cached data
func (s *Song) GetPreCache() ([]byte, context.CancelFunc) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PreCacheData, s.PreCacheCancel
}

// ClearPreCache safely clears pre-cached data and cancels pre-caching
func (s *Song) ClearPreCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PreCacheCancel != nil {
		s.PreCacheCancel()
		s.PreCacheCancel = nil
	}
	s.PreCacheData = nil
	logger.Debugf("[Song] Pre-cache cleared for %s", s.Title)
}

// UpdatePlaybackPosition updates seek time based on elapsed time
func (s *Song) UpdatePlaybackPosition() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.PlaybackStarted.IsZero() {
		elapsed := time.Since(s.PlaybackStarted)
		return s.SeekTime + int(elapsed.Milliseconds())
	}
	return s.SeekTime
}

// StartPlayback marks the song as playing and records start time
func (s *Song) StartPlayback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = SongStatePlaying
	s.PlaybackStarted = time.Now().Add(-time.Duration(s.SeekTime) * time.Millisecond)
	s.StateChangedAt = time.Now()
	logger.Infof("[Song] Started playback: %s", s.Title)
}

// PausePlayback updates seek time and marks as paused
func (s *Song) PausePlayback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.PlaybackStarted.IsZero() {
		elapsed := time.Since(s.PlaybackStarted)
		s.SeekTime = int(elapsed.Milliseconds())
	}
	s.State = SongStatePaused
	s.StateChangedAt = time.Now()
	logger.Infof("[Song] Paused at %dms: %s", s.SeekTime, s.Title)
}

// getStateName returns human-readable state name
func (s *Song) getStateName() string {
	switch s.State {
	case SongStateQueued:
		return "queued"
	case SongStateLoading:
		return "loading"
	case SongStatePlaying:
		return "playing"
	case SongStatePaused:
		return "paused"
	case SongStateFailed:
		return "failed"
	case SongStateCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

// Clone creates a deep copy of the song (for safe concurrent access)
func (s *Song) Clone() *Song {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := &Song{
		ID:             s.ID,
		GuildID:        s.GuildID,
		URL:            s.URL,
		Title:          s.Title,
		Duration:       s.Duration,
		Thumbnail:      s.Thumbnail,
		RequestedByID:  s.RequestedByID,
		RequestedByTag: s.RequestedByTag,
		QueuePosition:  s.QueuePosition,
		SeekTime:       s.SeekTime,
		Uploader:       s.Uploader,
		IsLive:         s.IsLive,
		State:          s.State,
		RetryCount:     s.RetryCount,
		AddedAt:        s.AddedAt,
		StateChangedAt: s.StateChangedAt,
	}

	// Note: Don't clone runtime resources like messages, cache data, or cancel functions
	// Those should be managed by the original song instance

	return clone
}

// Queue represents a guild's music queue
type Queue struct {
	GuildID          string
	TextChannelID    string
	VoiceChannelID   string
	Songs            []*Song
	Volume           float64
	RepeatMode       int
	SponsorBlock     bool
	ShowStartedTrack bool
	Normalization    bool
	Paused           bool
	Playing          bool
	Loading          bool
}

// Cache for active queues
type queueCache struct {
	queue     *Queue
	timestamp time.Time
}

var (
	// In-memory cache with 30s TTL
	cache    = make(map[string]*queueCache)
	cacheMux sync.RWMutex
	cacheTTL = 30 * time.Second

	// Per-guild locks for operations
	locks    = make(map[string]*sync.Mutex)
	locksMux sync.Mutex
)

// acquireLock gets or creates a mutex for a guild
func acquireLock(guildID string) *sync.Mutex {
	locksMux.Lock()
	defer locksMux.Unlock()

	if _, exists := locks[guildID]; !exists {
		locks[guildID] = &sync.Mutex{}
	}
	return locks[guildID]
}

// GetQueue retrieves a queue from cache or database
func GetQueue(guildID string, forceRefresh bool) (*Queue, error) {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Check cache
	if !forceRefresh {
		cacheMux.RLock()
		cached, exists := cache[guildID]
		cacheMux.RUnlock()

		if exists && time.Since(cached.timestamp) < cacheTTL {
			logger.Debugf("[Queue] Using cached queue for guild: %s", guildID)
			return cached.queue, nil
		}
	}

	// Fetch from database
	queue, err := loadQueueFromDB(guildID)
	if err != nil {
		return nil, err
	}

	if queue == nil {
		return nil, nil
	}

	// Update cache
	cacheMux.Lock()
	cache[guildID] = &queueCache{
		queue:     queue,
		timestamp: time.Now(),
	}
	cacheMux.Unlock()

	logger.Debugf("[Queue] Loaded queue for guild %s: %d songs", guildID, len(queue.Songs))
	return queue, nil
}

// loadQueueFromDB loads a queue from the database
func loadQueueFromDB(guildID string) (*Queue, error) {
	// Get queue metadata
	var textChannelID, voiceChannelID string
	var paused, playing, loading int

	err := database.DB.QueryRow(
		`SELECT text_channel_id, voice_channel_id, paused,
		 COALESCE(playing, 0), COALESCE(loading, 0)
		 FROM queues WHERE guild_id = ?`,
		guildID,
	).Scan(&textChannelID, &voiceChannelID, &paused, &playing, &loading)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query queue: %w", err)
	}

	// Get guild settings
	var volume float64
	var repeat, sponsorblock, showStartedTrack, normalization int
	err = database.DB.QueryRow(
		`SELECT volume, repeat, sponsorblock, show_started_track, normalization
		 FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&volume, &repeat, &sponsorblock, &showStartedTrack, &normalization)

	if err == sql.ErrNoRows {
		// Use defaults
		cfg := config.GetConfig()
		if cfg != nil {
			volume = cfg.DefaultVolume
		} else {
			volume = 100 // fallback default
		}
		repeat = 0
		sponsorblock = 0
		showStartedTrack = 1
		normalization = 0
		logger.Debugf("[LoadQueue] No guild_settings found for guild %s, using defaults (volume=%g)", guildID, volume)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query guild settings: %w", err)
	} else {
		logger.Debugf("[LoadQueue] Loaded guild_settings for guild %s: volume=%g, repeat=%t, sponsorblock=%t, normalization=%t",
			guildID, volume, repeat == 1, sponsorblock == 1, normalization == 1)
	}

	// Get songs
	rows, err := database.DB.Query(
		`SELECT id, guild_id, url, title, duration, thumbnail, requested_by_id,
		 requested_by_tag, queue_position, seek_time, uploader, is_live
		 FROM songs WHERE guild_id = ? ORDER BY queue_position ASC`,
		guildID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query songs: %w", err)
	}
	defer rows.Close()

	songs := []*Song{}
	for rows.Next() {
		var song Song
		var isLive int
		err := rows.Scan(
			&song.ID, &song.GuildID, &song.URL, &song.Title, &song.Duration,
			&song.Thumbnail, &song.RequestedByID, &song.RequestedByTag,
			&song.QueuePosition, &song.SeekTime, &song.Uploader, &isLive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan song: %w", err)
		}
		song.IsLive = isLive == 1
		songs = append(songs, &song)
	}

	queue := &Queue{
		GuildID:          guildID,
		TextChannelID:    textChannelID,
		VoiceChannelID:   voiceChannelID,
		Songs:            songs,
		Volume:           volume,
		RepeatMode:       repeat,
		SponsorBlock:     sponsorblock == 1,
		ShowStartedTrack: showStartedTrack == 1,
		Normalization:    normalization == 1,
		Paused:           paused == 1,
		Playing:          playing == 1,
		Loading:          loading == 1,
	}

	return queue, nil
}

// CreateQueue creates a new queue in the database
func CreateQueue(guildID, textChannelID, voiceChannelID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Delete old queue if exists
	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete old songs: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete old queue: %w", err)
	}

	// Create new queue
	_, err = database.DB.Exec(
		`INSERT INTO queues (guild_id, text_channel_id, voice_channel_id) VALUES (?, ?, ?)`,
		guildID, textChannelID, voiceChannelID,
	)
	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[CreateQueue] Queue created for guild: %s", guildID)
	return nil
}

// DeleteQueue deletes a queue and all its songs
func DeleteQueue(guildID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[DeleteQueue] Queue deleted for guild: %s", guildID)
	return nil
}

// DeleteGuildData deletes all data for a guild (queue, songs, and settings)
// This should be called when the bot leaves or is removed from a guild
func DeleteGuildData(guildID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Delete songs
	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	// Delete queue
	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	// Delete guild settings
	_, err = database.DB.Exec(`DELETE FROM guild_settings WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete guild settings: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Infof("[DeleteGuildData] All data deleted for guild: %s", guildID)
	return nil
}

// AddSongsBatch adds multiple songs to the queue in a single batch operation
// This is much more efficient than calling AddSong multiple times for playlists
func AddSongsBatch(guildID string, songs []*Song, position int) error {
	if len(songs) == 0 {
		return nil
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Check if queue exists
	var queueExists int
	err := database.DB.QueryRow(`SELECT 1 FROM queues WHERE guild_id = ? LIMIT 1`, guildID).Scan(&queueExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("queue does not exist for guild: %s", guildID)
	} else if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	// Get current queue length
	var count int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count songs: %w", err)
	}

	// Determine position
	if position == -1 || position > count {
		position = count
	}

	// Make space for new songs
	if position < count {
		_, err = database.DB.Exec(
			`UPDATE songs SET queue_position = queue_position + ?
			 WHERE guild_id = ? AND queue_position >= ?`,
			len(songs), guildID, position,
		)
		if err != nil {
			return fmt.Errorf("failed to update positions: %w", err)
		}
	}

	// Build batch INSERT query
	// SQLite supports up to 999 parameters per query, each song has 10 parameters
	// So we can insert up to 99 songs per batch (990 parameters)
	const maxSongsPerBatch = 99

	for batchStart := 0; batchStart < len(songs); batchStart += maxSongsPerBatch {
		batchEnd := batchStart + maxSongsPerBatch
		if batchEnd > len(songs) {
			batchEnd = len(songs)
		}
		batch := songs[batchStart:batchEnd]

		// Build query with placeholders
		query := `INSERT INTO songs (guild_id, url, title, duration, thumbnail, requested_by_id,
			requested_by_tag, queue_position, uploader, is_live) VALUES `

		values := []interface{}{}
		for i, song := range batch {
			if i > 0 {
				query += ", "
			}
			query += "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

			isLiveInt := 0
			if song.IsLive {
				isLiveInt = 1
			}

			values = append(values,
				guildID, song.URL, song.Title, song.Duration, song.Thumbnail,
				song.RequestedByID, song.RequestedByTag, position+batchStart+i, song.Uploader, isLiveInt,
			)
		}

		// Execute batch insert
		_, err = database.DB.Exec(query, values...)
		if err != nil {
			return fmt.Errorf("failed to batch insert songs (batch %d): %w", batchStart/maxSongsPerBatch+1, err)
		}
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Infof("[AddSongsBatch] Added %d songs starting at position %d for guild: %s", len(songs), position, guildID)
	return nil
}

// AddSong adds a song to the queue
func AddSong(guildID string, song *Song, position int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Check if queue exists
	var queueExists int
	err := database.DB.QueryRow(`SELECT 1 FROM queues WHERE guild_id = ? LIMIT 1`, guildID).Scan(&queueExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("queue does not exist for guild: %s", guildID)
	} else if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	// Check for duplicate URL
	var existingID int
	err = database.DB.QueryRow(`SELECT id FROM songs WHERE guild_id = ? AND url = ? LIMIT 1`, guildID, song.URL).Scan(&existingID)
	if err == nil {
		// Song already exists
		return fmt.Errorf("song already in queue: %s", song.Title)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check for duplicate: %w", err)
	}

	// Get current queue length
	var count int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count songs: %w", err)
	}

	// Determine position
	if position == -1 || position > count {
		position = count
	}

	// Make space for new song
	if position < count {
		_, err = database.DB.Exec(
			`UPDATE songs SET queue_position = queue_position + 1
			 WHERE guild_id = ? AND queue_position >= ?`,
			guildID, position,
		)
		if err != nil {
			return fmt.Errorf("failed to update positions: %w", err)
		}
	}

	// Insert song
	isLiveInt := 0
	if song.IsLive {
		isLiveInt = 1
	}

	_, err = database.DB.Exec(
		`INSERT INTO songs (guild_id, url, title, duration, thumbnail, requested_by_id,
		 requested_by_tag, queue_position, uploader, is_live)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		guildID, song.URL, song.Title, song.Duration, song.Thumbnail,
		song.RequestedByID, song.RequestedByTag, position, song.Uploader, isLiveInt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert song: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[AddSong] Added song '%s' at position %d for guild: %s", song.Title, position, guildID)
	return nil
}

// UpdateSongSeekTime updates the seek time for a song (used for crash recovery)
func UpdateSongSeekTime(guildID string, songID int, seekTime int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`UPDATE songs SET seek_time = ? WHERE guild_id = ? AND id = ?`,
		seekTime, guildID, songID,
	)
	if err != nil {
		return fmt.Errorf("failed to update seek time: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[UpdateSongSeekTime] Updated seek time to %dms for song %d in guild: %s", seekTime, songID, guildID)
	return nil
}

// RemoveFirstSong removes the first song from the queue
func RemoveFirstSong(guildID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Begin transaction for atomic delete + reorder
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	// Get first song ID
	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? ORDER BY queue_position ASC LIMIT 1`,
		guildID,
	).Scan(&songID)

	if err == sql.ErrNoRows {
		return nil // No songs to remove
	}
	if err != nil {
		return fmt.Errorf("failed to get first song: %w", err)
	}

	// Delete first song
	_, err = tx.Exec(`DELETE FROM songs WHERE id = ?`, songID)
	if err != nil {
		return fmt.Errorf("failed to delete song: %w", err)
	}

	// Update positions
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = queue_position - 1 WHERE guild_id = ?`,
		guildID,
	)
	if err != nil {
		return fmt.Errorf("failed to update positions: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[RemoveFirstSong] Removed first song for guild: %s", guildID)
	return nil
}

// RemoveSong removes a song at a specific position
func RemoveSong(guildID string, position int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Begin transaction for atomic delete + reorder
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get song ID at position
	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, position,
	).Scan(&songID)

	if err != nil {
		return fmt.Errorf("failed to find song at position %d: %w", position, err)
	}

	// Delete song
	_, err = tx.Exec(`DELETE FROM songs WHERE id = ?`, songID)
	if err != nil {
		return fmt.Errorf("failed to delete song: %w", err)
	}

	// Update positions
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = queue_position - 1
		 WHERE guild_id = ? AND queue_position > ?`,
		guildID, position,
	)
	if err != nil {
		return fmt.Errorf("failed to update positions: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[RemoveSong] Removed song at position %d for guild: %s", position, guildID)
	return nil
}

// SkipToPosition removes all songs before targetIndex, making the song at targetIndex the new first song
func SkipToPosition(guildID string, targetIndex int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Begin transaction for atomic delete + reorder
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get all songs before target position
	rows, err := tx.Query(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position < ? ORDER BY queue_position ASC`,
		guildID, targetIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to query songs: %w", err)
	}
	defer rows.Close()

	var songIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		songIDs = append(songIDs, id)
	}
	rows.Close()

	if len(songIDs) == 0 {
		return nil // Nothing to skip
	}

	// Delete songs before target
	placeholders := make([]string, len(songIDs))
	args := make([]interface{}, len(songIDs))
	for i, id := range songIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`DELETE FROM songs WHERE id IN (%s)`, strings.Join(placeholders, ","))
	_, err = tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	// Reorder remaining songs starting from 0
	if err := reorderSongsAfterRemovalTx(tx, guildID); err != nil {
		return fmt.Errorf("failed to reorder songs: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[SkipToPosition] Skipped to position %d for guild: %s", targetIndex, guildID)
	return nil
}

// RemoveSongsByIDs removes multiple songs by their IDs
func RemoveSongsByIDs(guildID string, songIDs []int) error {
	if len(songIDs) == 0 {
		return nil
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Begin transaction for atomic delete + reorder
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Build placeholders for SQL IN clause
	placeholders := make([]string, len(songIDs))
	args := make([]interface{}, len(songIDs)+1)
	args[0] = guildID

	for i, id := range songIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(`DELETE FROM songs WHERE guild_id = ? AND id IN (%s)`,
		strings.Join(placeholders, ","))

	result, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete songs: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	// Reorder remaining songs
	if err := reorderSongsAfterRemovalTx(tx, guildID); err != nil {
		return fmt.Errorf("failed to reorder songs: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[RemoveSongsByIDs] Removed %d songs for guild: %s", rowsAffected, guildID)
	return nil
}

// reorderSongsAfterRemoval fixes queue positions after bulk removal
func reorderSongsAfterRemoval(guildID string) error {
	// Get all remaining songs ordered by position
	rows, err := database.DB.Query(
		`SELECT id FROM songs WHERE guild_id = ? ORDER BY queue_position ASC`,
		guildID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var songIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		songIDs = append(songIDs, id)
	}

	// Update positions sequentially
	for i, id := range songIDs {
		_, err := database.DB.Exec(
			`UPDATE songs SET queue_position = ? WHERE id = ?`,
			i, id,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// reorderSongsAfterRemovalTx fixes queue positions after bulk removal within a transaction
func reorderSongsAfterRemovalTx(tx *sql.Tx, guildID string) error {
	// Get all remaining songs ordered by position
	rows, err := tx.Query(
		`SELECT id FROM songs WHERE guild_id = ? ORDER BY queue_position ASC`,
		guildID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var songIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		songIDs = append(songIDs, id)
	}
	rows.Close()

	// Update positions sequentially
	for i, id := range songIDs {
		_, err := tx.Exec(
			`UPDATE songs SET queue_position = ? WHERE id = ?`,
			i, id,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// SetRepeatMode sets the repeat mode for a guild (RepeatOff=0, RepeatAll=1, RepeatSingle=2)
func SetRepeatMode(guildID string, mode int) error {
	if mode < RepeatOff || mode > RepeatSingle {
		return fmt.Errorf("invalid repeat mode: %d", mode)
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, repeat) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET repeat = ?`,
		guildID, mode, mode,
	)
	if err != nil {
		return fmt.Errorf("failed to set repeat mode: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[SetRepeatMode] Set repeat=%d for guild: %s", mode, guildID)
	return nil
}

// SetVolume sets the volume for a guild
func SetVolume(guildID string, volume float64) error {
	// Check for invalid float values
	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		return fmt.Errorf("volume must be a valid number, got: %g", volume)
	}

	// Validate volume range (0-1000)
	if volume < 0 || volume > 1000 {
		return fmt.Errorf("volume must be between 0 and 1000, got: %g", volume)
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	result, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, volume) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET volume = ?`,
		guildID, volume, volume,
	)
	if err != nil {
		logger.Errorf("[SetVolume] Database error for guild %s: %v", guildID, err)
		return fmt.Errorf("failed to set volume: %w", err)
	}

	// Log the result for debugging
	rowsAffected, _ := result.RowsAffected()
	logger.Infof("[SetVolume] Set volume=%g for guild %s (rows affected: %d)", volume, guildID, rowsAffected)

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[Cache] Invalidated cache for guild: %s", guildID)
	return nil
}

// GetVolume gets the volume setting for a guild from database
func GetVolume(guildID string) (float64, error) {
	var volume float64
	err := database.DB.QueryRow(
		`SELECT volume FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&volume)

	if err == sql.ErrNoRows {
		// No settings found, return default
		cfg := config.GetConfig()
		if cfg != nil {
			return cfg.DefaultVolume, nil
		}
		return 100, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get volume: %w", err)
	}

	return volume, nil
}

// GetRepeatMode gets the repeat mode for a guild from database (RepeatOff=0, RepeatAll=1, RepeatSingle=2)
func GetRepeatMode(guildID string) (int, error) {
	var repeat int
	err := database.DB.QueryRow(
		`SELECT repeat FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&repeat)

	if err == sql.ErrNoRows {
		return RepeatOff, nil
	}
	if err != nil {
		return RepeatOff, fmt.Errorf("failed to get repeat mode: %w", err)
	}

	return repeat, nil
}

// GetSponsorBlock gets the SponsorBlock setting for a guild from database
func GetSponsorBlock(guildID string) (bool, error) {
	var sponsorblock int
	err := database.DB.QueryRow(
		`SELECT sponsorblock FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&sponsorblock)

	if err == sql.ErrNoRows {
		// No settings found, return default (false)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get sponsorblock: %w", err)
	}

	return sponsorblock == 1, nil
}

// GetShowStartedTrack gets the ShowStartedTrack setting for a guild from database
func GetShowStartedTrack(guildID string) (bool, error) {
	var showStartedTrack int
	err := database.DB.QueryRow(
		`SELECT show_started_track FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&showStartedTrack)

	if err == sql.ErrNoRows {
		// No settings found, return default (true - enabled by default)
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get show_started_track: %w", err)
	}

	return showStartedTrack == 1, nil
}

// GetNormalization gets the Normalization setting for a guild from database
func GetNormalization(guildID string) (bool, error) {
	var normalization int
	err := database.DB.QueryRow(
		`SELECT normalization FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&normalization)

	if err == sql.ErrNoRows {
		// No settings found, return default (false)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get normalization: %w", err)
	}

	return normalization == 1, nil
}

// SetSponsorBlock sets the SponsorBlock mode for a guild
func SetSponsorBlock(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	sponsorblockInt := 0
	if enabled {
		sponsorblockInt = 1
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, sponsorblock) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET sponsorblock = ?`,
		guildID, sponsorblockInt, sponsorblockInt,
	)
	if err != nil {
		return fmt.Errorf("failed to set sponsorblock: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[SetSponsorBlock] Set sponsorblock=%v for guild: %s", enabled, guildID)
	return nil
}

// SetShowStartedTrack sets the ShowStartedTrack mode for a guild
func SetShowStartedTrack(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	showStartedTrackInt := 0
	if enabled {
		showStartedTrackInt = 1
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, show_started_track) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET show_started_track = ?`,
		guildID, showStartedTrackInt, showStartedTrackInt,
	)
	if err != nil {
		return fmt.Errorf("failed to set show_started_track: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[SetShowStartedTrack] Set show_started_track=%v for guild: %s", enabled, guildID)
	return nil
}

// SetNormalization sets the Normalization mode for a guild
func SetNormalization(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	normalizationInt := 0
	if enabled {
		normalizationInt = 1
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, normalization) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET normalization = ?`,
		guildID, normalizationInt, normalizationInt,
	)
	if err != nil {
		return fmt.Errorf("failed to set normalization: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[SetNormalization] Set normalization=%v for guild: %s", enabled, guildID)
	return nil
}

// SwapSongs swaps two songs by position
func SwapSongs(guildID string, pos1, pos2 int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Validate positions
	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if pos1 < 0 || pos2 < 0 || pos1 >= len(q.Songs) || pos2 >= len(q.Songs) {
		return fmt.Errorf("invalid positions")
	}

	if pos1 == pos2 {
		return nil // Nothing to swap
	}

	// Begin transaction for atomic swap
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get song IDs at both positions
	var song1ID, song2ID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, pos1,
	).Scan(&song1ID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", pos1, err)
	}

	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, pos2,
	).Scan(&song2ID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", pos2, err)
	}

	// Swap positions in database using a temporary position to avoid conflicts
	tempPos := -1

	// Move song1 to temp
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, song1ID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to temp: %w", err)
	}

	// Move song2 to pos1
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos1, song2ID)
	if err != nil {
		return fmt.Errorf("failed to move song2 to pos1: %w", err)
	}

	// Move song1 from temp to pos2
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos2, song1ID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to pos2: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SwapSongs] Swapped songs at positions %d and %d for guild: %s", pos1, pos2, guildID)
	return nil
}

// MoveSong moves a song from one position to another
func MoveSong(guildID string, fromPos, toPos int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	// Validate positions
	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if fromPos < 0 || toPos < 0 || fromPos >= len(q.Songs) || toPos >= len(q.Songs) {
		return fmt.Errorf("invalid positions")
	}

	if fromPos == toPos {
		return nil // Nothing to move
	}

	// Begin transaction for atomic move
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get song ID at fromPos
	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, fromPos,
	).Scan(&songID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", fromPos, err)
	}

	// Move song to temporary position first
	tempPos := -1
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to temp: %w", err)
	}

	// Shift other songs
	if fromPos < toPos {
		// Moving down: shift songs between fromPos and toPos up by 1
		_, err = tx.Exec(
			`UPDATE songs SET queue_position = queue_position - 1
			 WHERE guild_id = ? AND queue_position > ? AND queue_position <= ?`,
			guildID, fromPos, toPos)
	} else {
		// Moving up: shift songs between toPos and fromPos down by 1
		_, err = tx.Exec(
			`UPDATE songs SET queue_position = queue_position + 1
			 WHERE guild_id = ? AND queue_position >= ? AND queue_position < ?`,
			guildID, toPos, fromPos)
	}
	if err != nil {
		return fmt.Errorf("failed to shift songs: %w", err)
	}

	// Move song from temp to toPos
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		toPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to final position: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[MoveSong] Moved song from position %d to %d for guild: %s", fromPos, toPos, guildID)
	return nil
}

// SaveSeekTime saves the current seek position for a song
func SaveSeekTime(guildID string, songID int, seekTime int) (int, error) {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`UPDATE songs SET seek_time = ? WHERE id = ? AND guild_id = ?`,
		seekTime, songID, guildID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to save seek time: %w", err)
	}

	// Invalidate cache
	InvalidateCache(guildID)
	logger.Debugf("[SaveSeekTime] Saved seek time %dms for song %d in guild: %s", seekTime, songID, guildID)
	return seekTime, nil
}

// UpdateVoiceChannel updates the voice channel ID for a queue
func UpdateVoiceChannel(guildID, channelID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`UPDATE queues SET voice_channel_id = ? WHERE guild_id = ?`,
		channelID, guildID)
	if err != nil {
		return fmt.Errorf("failed to update voice channel: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[UpdateVoiceChannel] Updated voice channel to %s for guild: %s", channelID, guildID)
	return nil
}

// SetPaused sets the paused state for a queue
func SetPaused(guildID string, paused bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	pausedInt := 0
	if paused {
		pausedInt = 1
	}

	_, err := database.DB.Exec(
		`UPDATE queues SET paused = ? WHERE guild_id = ?`,
		pausedInt, guildID)
	if err != nil {
		return fmt.Errorf("failed to set paused state: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetPaused] Set paused=%v for guild: %s", paused, guildID)
	return nil
}

// SetPlaying sets the playing state for a queue
func SetPlaying(guildID string, playing bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	playingInt := 0
	if playing {
		playingInt = 1
	}

	_, err := database.DB.Exec(
		`UPDATE queues SET playing = ? WHERE guild_id = ?`,
		playingInt, guildID)
	if err != nil {
		return fmt.Errorf("failed to set playing state: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetPlaying] Set playing=%v for guild: %s", playing, guildID)
	return nil
}

// SetLoading sets the loading state for a queue
func SetLoading(guildID string, loading bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	loadingInt := 0
	if loading {
		loadingInt = 1
	}

	_, err := database.DB.Exec(
		`UPDATE queues SET loading = ? WHERE guild_id = ?`,
		loadingInt, guildID)
	if err != nil {
		return fmt.Errorf("failed to set loading state: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetLoading] Set loading=%v for guild: %s", loading, guildID)
	return nil
}

// InvalidateCache removes a guild's queue from cache
func InvalidateCache(guildID string) {
	cacheMux.Lock()
	defer cacheMux.Unlock()
	delete(cache, guildID)
	logger.Debugf("[Cache] Invalidated cache for guild: %s", guildID)
}
