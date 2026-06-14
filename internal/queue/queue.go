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

type SongState int

const (
	SongStateQueued SongState = iota 
	SongStateLoading                 
	SongStatePlaying                 
	SongStatePaused                  
	SongStateFailed                  
	SongStateCompleted               
)

const (
	RepeatOff    = 0 
	RepeatAll    = 1 
	RepeatSingle = 2 
)

type Song struct {
	
	ID             int
	GuildID        string
	URL            string
	Title          string
	Duration       string
	Thumbnail      string
	RequestedByID  string
	RequestedByTag string
	QueuePosition  int
	SeekTime       int    
	Uploader       string
	IsLive         bool

	
	State           SongState              
	RetryCount      int                    
	LastError       error                  
	LoadingMessage  *discordgo.Message     
	PreCacheData    []byte                 
	PreCacheCancel  context.CancelFunc     
	PlaybackStarted time.Time              
	AddedAt         time.Time              
	StateChangedAt  time.Time              
	mu              sync.RWMutex           
}

func (s *Song) SetState(state SongState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
	s.StateChangedAt = time.Now()
	logger.Debugf("[Song] %s state changed to %s", s.Title, s.getStateName())
}

func (s *Song) GetState() SongState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

func (s *Song) IncrementRetry() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RetryCount++
	return s.RetryCount
}

func (s *Song) GetRetryCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RetryCount
}

func (s *Song) ResetRetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RetryCount = 0
}

func (s *Song) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastError = err
	if err != nil {
		logger.Errorf("[Song] %s encountered error: %v", s.Title, err)
	}
}

func (s *Song) GetError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastError
}

func (s *Song) SetLoadingMessage(msg *discordgo.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LoadingMessage = msg
}

func (s *Song) GetLoadingMessage() *discordgo.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LoadingMessage
}

func (s *Song) ClearLoadingMessage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LoadingMessage = nil
}

func (s *Song) SetPreCache(data []byte, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PreCacheData = data
	s.PreCacheCancel = cancel
	logger.Debugf("[Song] Pre-cache set for %s (%d bytes)", s.Title, len(data))
}

func (s *Song) GetPreCache() ([]byte, context.CancelFunc) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PreCacheData, s.PreCacheCancel
}

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

func (s *Song) UpdatePlaybackPosition() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.PlaybackStarted.IsZero() {
		elapsed := time.Since(s.PlaybackStarted)
		return s.SeekTime + int(elapsed.Milliseconds())
	}
	return s.SeekTime
}

func (s *Song) StartPlayback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = SongStatePlaying
	s.PlaybackStarted = time.Now().Add(-time.Duration(s.SeekTime) * time.Millisecond)
	s.StateChangedAt = time.Now()
	logger.Debugf("[Song] Started playback: %s", s.Title)
}

func (s *Song) PausePlayback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.PlaybackStarted.IsZero() {
		elapsed := time.Since(s.PlaybackStarted)
		s.SeekTime = int(elapsed.Milliseconds())
	}
	s.State = SongStatePaused
	s.StateChangedAt = time.Now()
	logger.Debugf("[Song] Paused at %dms: %s", s.SeekTime, s.Title)
}

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

	
	

	return clone
}

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
	FadeIn           bool
	FadeOut          bool
	AutoMix          bool
	FadeOnStop       bool
	FadeInDuration   float64
	FadeOutDuration  float64
	AutoMixBeats     int
	Crossfade        bool
	CrossfadeDuration float64
	TrimSilence      bool
}

type queueCache struct {
	queue     *Queue
	timestamp time.Time
}

var (
	
	cache    = make(map[string]*queueCache)
	cacheMux sync.RWMutex
	cacheTTL = 30 * time.Second

	
	locks    = make(map[string]*sync.Mutex)
	locksMux sync.Mutex

	prefixCache       = make(map[string]string)
	prefixCacheLoaded = make(map[string]bool)
	prefixCacheMux    sync.RWMutex
)

func acquireLock(guildID string) *sync.Mutex {
	locksMux.Lock()
	defer locksMux.Unlock()

	if _, exists := locks[guildID]; !exists {
		locks[guildID] = &sync.Mutex{}
	}
	return locks[guildID]
}

func GetQueue(guildID string, forceRefresh bool) (*Queue, error) {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	if !forceRefresh {
		cacheMux.RLock()
		cached, exists := cache[guildID]
		cacheMux.RUnlock()

		if exists && time.Since(cached.timestamp) < cacheTTL {
			logger.Debugf("[Queue] Using cached queue for guild: %s", guildID)
			return cached.queue, nil
		}
	}

	
	queue, err := loadQueueFromDB(guildID)
	if err != nil {
		return nil, err
	}

	if queue == nil {
		return nil, nil
	}

	
	cacheMux.Lock()
	cache[guildID] = &queueCache{
		queue:     queue,
		timestamp: time.Now(),
	}
	cacheMux.Unlock()

	logger.Debugf("[Queue] Loaded queue for guild %s: %d songs", guildID, len(queue.Songs))
	return queue, nil
}

func loadQueueFromDB(guildID string) (*Queue, error) {
	
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

	
	var volume float64
	var repeat, sponsorblock, showStartedTrack, normalization int
	var fadein, fadeout, automix, fadeOnStop, automixBeats, crossfade, trimSilence int
	var fadeinDuration, fadeoutDuration, crossfadeDuration float64
	err = database.DB.QueryRow(
		`SELECT volume, repeat, sponsorblock, show_started_track, normalization,
		 COALESCE(fadein, 0), COALESCE(fadeout, 0), COALESCE(automix, 0),
		 COALESCE(fade_on_stop, 0), COALESCE(fadein_duration, 3),
		 COALESCE(fadeout_duration, 3), COALESCE(automix_beats, 16),
		 COALESCE(crossfade, 0), COALESCE(crossfade_duration, 8),
		 COALESCE(trim_silence, 0)
		 FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&volume, &repeat, &sponsorblock, &showStartedTrack, &normalization,
		&fadein, &fadeout, &automix, &fadeOnStop, &fadeinDuration,
		&fadeoutDuration, &automixBeats, &crossfade, &crossfadeDuration,
		&trimSilence)

	if err == sql.ErrNoRows {

		cfg := config.GetConfig()
		if cfg != nil {
			volume = cfg.DefaultVolume
		} else {
			volume = 100
		}
		repeat = 0
		sponsorblock = 0
		showStartedTrack = 1
		normalization = 0
		fadein = 0
		fadeout = 0
		automix = 0
		fadeOnStop = 0
		fadeinDuration = 3
		fadeoutDuration = 3
		automixBeats = 16
		crossfade = 0
		crossfadeDuration = 8
		trimSilence = 0
		logger.Debugf("[LoadQueue] No guild_settings found for guild %s, using defaults (volume=%g)", guildID, volume)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query guild settings: %w", err)
	} else {
		logger.Debugf("[LoadQueue] Loaded guild_settings for guild %s: volume=%g, repeat=%t, sponsorblock=%t, normalization=%t",
			guildID, volume, repeat == 1, sponsorblock == 1, normalization == 1)
	}

	
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
		FadeIn:           fadein == 1,
		FadeOut:          fadeout == 1,
		AutoMix:          automix == 1,
		FadeOnStop:       fadeOnStop == 1,
		FadeInDuration:   fadeinDuration,
		FadeOutDuration:  fadeoutDuration,
		AutoMixBeats:     automixBeats,
		Crossfade:        crossfade == 1,
		CrossfadeDuration: crossfadeDuration,
		TrimSilence:      trimSilence == 1,
	}

	return queue, nil
}

func CreateQueue(guildID, textChannelID, voiceChannelID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	_, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete old songs: %w", err)
	}

	_, err = database.DB.Exec(`DELETE FROM queues WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete old queue: %w", err)
	}

	
	_, err = database.DB.Exec(
		`INSERT INTO queues (guild_id, text_channel_id, voice_channel_id) VALUES (?, ?, ?)`,
		guildID, textChannelID, voiceChannelID,
	)
	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	
	InvalidateCache(guildID)
	logger.Debugf("[CreateQueue] Queue created for guild: %s", guildID)
	return nil
}

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

	
	InvalidateCache(guildID)
	logger.Debugf("[DeleteQueue] Queue deleted for guild: %s", guildID)
	return nil
}

func DeleteGuildData(guildID string) error {
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

	
	_, err = database.DB.Exec(`DELETE FROM guild_settings WHERE guild_id = ?`, guildID)
	if err != nil {
		return fmt.Errorf("failed to delete guild settings: %w", err)
	}

	
	InvalidateCache(guildID)
	invalidatePrefixCache(guildID)
	logger.Infof("[DeleteGuildData] All data deleted for guild: %s", guildID)
	return nil
}

func AddSongsBatch(guildID string, songs []*Song, position int) error {
	if len(songs) == 0 {
		return nil
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	var queueExists int
	err := database.DB.QueryRow(`SELECT 1 FROM queues WHERE guild_id = ? LIMIT 1`, guildID).Scan(&queueExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("queue does not exist for guild: %s", guildID)
	} else if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	
	var count int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count songs: %w", err)
	}

	
	if position == -1 || position > count {
		position = count
	}

	
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

	
	
	
	const maxSongsPerBatch = 99

	for batchStart := 0; batchStart < len(songs); batchStart += maxSongsPerBatch {
		batchEnd := batchStart + maxSongsPerBatch
		if batchEnd > len(songs) {
			batchEnd = len(songs)
		}
		batch := songs[batchStart:batchEnd]

		
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

		
		_, err = database.DB.Exec(query, values...)
		if err != nil {
			return fmt.Errorf("failed to batch insert songs (batch %d): %w", batchStart/maxSongsPerBatch+1, err)
		}
	}

	
	InvalidateCache(guildID)
	logger.Debugf("[AddSongsBatch] Added %d songs starting at position %d for guild: %s", len(songs), position, guildID)
	return nil
}

func AddSong(guildID string, song *Song, position int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	var queueExists int
	err := database.DB.QueryRow(`SELECT 1 FROM queues WHERE guild_id = ? LIMIT 1`, guildID).Scan(&queueExists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("queue does not exist for guild: %s", guildID)
	} else if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}

	
	var existingID int
	err = database.DB.QueryRow(`SELECT id FROM songs WHERE guild_id = ? AND url = ? LIMIT 1`, guildID, song.URL).Scan(&existingID)
	if err == nil {
		
		return fmt.Errorf("song already in queue: %s", song.Title)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check for duplicate: %w", err)
	}

	
	var count int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count songs: %w", err)
	}

	
	if position == -1 || position > count {
		position = count
	}

	
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

	
	InvalidateCache(guildID)
	logger.Debugf("[AddSong] Added song '%s' at position %d for guild: %s", song.Title, position, guildID)
	return nil
}

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

	
	InvalidateCache(guildID)
	logger.Debugf("[UpdateSongSeekTime] Updated seek time to %dms for song %d in guild: %s", seekTime, songID, guildID)
	return nil
}

func RemoveFirstSong(guildID string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() 

	
	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? ORDER BY queue_position ASC LIMIT 1`,
		guildID,
	).Scan(&songID)

	if err == sql.ErrNoRows {
		return nil 
	}
	if err != nil {
		return fmt.Errorf("failed to get first song: %w", err)
	}

	
	_, err = tx.Exec(`DELETE FROM songs WHERE id = ?`, songID)
	if err != nil {
		return fmt.Errorf("failed to delete song: %w", err)
	}

	
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = queue_position - 1 WHERE guild_id = ?`,
		guildID,
	)
	if err != nil {
		return fmt.Errorf("failed to update positions: %w", err)
	}

	
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	
	InvalidateCache(guildID)
	logger.Debugf("[RemoveFirstSong] Removed first song for guild: %s", guildID)
	return nil
}

func RemoveSong(guildID string, position int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	
	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, position,
	).Scan(&songID)

	if err != nil {
		return fmt.Errorf("failed to find song at position %d: %w", position, err)
	}

	
	_, err = tx.Exec(`DELETE FROM songs WHERE id = ?`, songID)
	if err != nil {
		return fmt.Errorf("failed to delete song: %w", err)
	}

	
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = queue_position - 1
		 WHERE guild_id = ? AND queue_position > ?`,
		guildID, position,
	)
	if err != nil {
		return fmt.Errorf("failed to update positions: %w", err)
	}

	
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	
	InvalidateCache(guildID)
	logger.Debugf("[RemoveSong] Removed song at position %d for guild: %s", position, guildID)
	return nil
}

func SkipToPosition(guildID string, targetIndex int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	
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
		return nil 
	}

	
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

	
	if err := reorderSongsAfterRemovalTx(tx, guildID); err != nil {
		return fmt.Errorf("failed to reorder songs: %w", err)
	}

	
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	
	InvalidateCache(guildID)
	logger.Debugf("[SkipToPosition] Skipped to position %d for guild: %s", targetIndex, guildID)
	return nil
}

func RemoveSongsByIDs(guildID string, songIDs []int) error {
	if len(songIDs) == 0 {
		return nil
	}

	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	
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

	
	if err := reorderSongsAfterRemovalTx(tx, guildID); err != nil {
		return fmt.Errorf("failed to reorder songs: %w", err)
	}

	
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	
	InvalidateCache(guildID)
	logger.Debugf("[RemoveSongsByIDs] Removed %d songs for guild: %s", rowsAffected, guildID)
	return nil
}

func reorderSongsAfterRemoval(guildID string) error {
	
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

func reorderSongsAfterRemovalTx(tx *sql.Tx, guildID string) error {
	
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

	
	InvalidateCache(guildID)
	logger.Debugf("[SetRepeatMode] Set repeat=%d for guild: %s", mode, guildID)
	return nil
}

func SetVolume(guildID string, volume float64) error {
	
	if math.IsNaN(volume) || math.IsInf(volume, 0) {
		return fmt.Errorf("volume must be a valid number, got: %g", volume)
	}

	
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

	rowsAffected, _ := result.RowsAffected()
	logger.Debugf("[SetVolume] Set volume=%g for guild %s (rows affected: %d)", volume, guildID, rowsAffected)

	
	InvalidateCache(guildID)
	logger.Debugf("[Cache] Invalidated cache for guild: %s", guildID)
	return nil
}

func GetVolume(guildID string) (float64, error) {
	var volume float64
	err := database.DB.QueryRow(
		`SELECT volume FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&volume)

	if err == sql.ErrNoRows {
		
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

func GetSponsorBlock(guildID string) (bool, error) {
	var sponsorblock int
	err := database.DB.QueryRow(
		`SELECT sponsorblock FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&sponsorblock)

	if err == sql.ErrNoRows {
		
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get sponsorblock: %w", err)
	}

	return sponsorblock == 1, nil
}

func GetShowStartedTrack(guildID string) (bool, error) {
	var showStartedTrack int
	err := database.DB.QueryRow(
		`SELECT show_started_track FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&showStartedTrack)

	if err == sql.ErrNoRows {
		
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get show_started_track: %w", err)
	}

	return showStartedTrack == 1, nil
}

func GetNormalization(guildID string) (bool, error) {
	var normalization int
	err := database.DB.QueryRow(
		`SELECT normalization FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&normalization)

	if err == sql.ErrNoRows {
		
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get normalization: %w", err)
	}

	return normalization == 1, nil
}

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

	
	InvalidateCache(guildID)
	logger.Debugf("[SetSponsorBlock] Set sponsorblock=%v for guild: %s", enabled, guildID)
	return nil
}

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

	
	InvalidateCache(guildID)
	logger.Debugf("[SetShowStartedTrack] Set show_started_track=%v for guild: %s", enabled, guildID)
	return nil
}

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


	InvalidateCache(guildID)
	logger.Debugf("[SetNormalization] Set normalization=%v for guild: %s", enabled, guildID)
	return nil
}

func boolToInt(enabled bool) int {
	if enabled {
		return 1
	}
	return 0
}

func GetFadeIn(guildID string) (bool, error) {
	var fadein int
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadein, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&fadein)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get fadein: %w", err)
	}

	return fadein == 1, nil
}

func SetFadeIn(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadein) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadein = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadein: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetFadeIn] Set fadein=%v for guild: %s", enabled, guildID)
	return nil
}

func GetFadeInDuration(guildID string) (float64, error) {
	var duration float64
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadein_duration, 3) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&duration)

	if err == sql.ErrNoRows {
		return 3, nil
	}
	if err != nil {
		return 3, fmt.Errorf("failed to get fadein_duration: %w", err)
	}

	return duration, nil
}

func SetFadeInDuration(guildID string, seconds float64) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadein_duration) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadein_duration = ?`,
		guildID, seconds, seconds,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadein_duration: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetFadeInDuration] Set fadein_duration=%g for guild: %s", seconds, guildID)
	return nil
}

func GetFadeOut(guildID string) (bool, error) {
	var fadeout int
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadeout, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&fadeout)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get fadeout: %w", err)
	}

	return fadeout == 1, nil
}

func SetFadeOut(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadeout) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadeout = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadeout: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetFadeOut] Set fadeout=%v for guild: %s", enabled, guildID)
	return nil
}

func GetFadeOutDuration(guildID string) (float64, error) {
	var duration float64
	err := database.DB.QueryRow(
		`SELECT COALESCE(fadeout_duration, 3) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&duration)

	if err == sql.ErrNoRows {
		return 3, nil
	}
	if err != nil {
		return 3, fmt.Errorf("failed to get fadeout_duration: %w", err)
	}

	return duration, nil
}

func SetFadeOutDuration(guildID string, seconds float64) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fadeout_duration) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fadeout_duration = ?`,
		guildID, seconds, seconds,
	)
	if err != nil {
		return fmt.Errorf("failed to set fadeout_duration: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetFadeOutDuration] Set fadeout_duration=%g for guild: %s", seconds, guildID)
	return nil
}

func GetAutoMix(guildID string) (bool, error) {
	var automix int
	err := database.DB.QueryRow(
		`SELECT COALESCE(automix, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&automix)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get automix: %w", err)
	}

	return automix == 1, nil
}

func SetAutoMix(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, automix) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET automix = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set automix: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetAutoMix] Set automix=%v for guild: %s", enabled, guildID)
	return nil
}

func GetAutoMixBeats(guildID string) (int, error) {
	var beats int
	err := database.DB.QueryRow(
		`SELECT COALESCE(automix_beats, 16) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&beats)

	if err == sql.ErrNoRows {
		return 16, nil
	}
	if err != nil {
		return 16, fmt.Errorf("failed to get automix_beats: %w", err)
	}

	return beats, nil
}

func SetAutoMixBeats(guildID string, beats int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, automix_beats) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET automix_beats = ?`,
		guildID, beats, beats,
	)
	if err != nil {
		return fmt.Errorf("failed to set automix_beats: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetAutoMixBeats] Set automix_beats=%d for guild: %s", beats, guildID)
	return nil
}

func GetCrossfade(guildID string) (bool, error) {
	var crossfade int
	err := database.DB.QueryRow(
		`SELECT COALESCE(crossfade, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&crossfade)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get crossfade: %w", err)
	}

	return crossfade == 1, nil
}

func SetCrossfade(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, crossfade) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET crossfade = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set crossfade: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetCrossfade] Set crossfade=%v for guild: %s", enabled, guildID)
	return nil
}

func GetCrossfadeDuration(guildID string) (float64, error) {
	var duration float64
	err := database.DB.QueryRow(
		`SELECT COALESCE(crossfade_duration, 8) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&duration)

	if err == sql.ErrNoRows {
		return 8, nil
	}
	if err != nil {
		return 8, fmt.Errorf("failed to get crossfade_duration: %w", err)
	}

	return duration, nil
}

func SetCrossfadeDuration(guildID string, seconds float64) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, crossfade_duration) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET crossfade_duration = ?`,
		guildID, seconds, seconds,
	)
	if err != nil {
		return fmt.Errorf("failed to set crossfade_duration: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetCrossfadeDuration] Set crossfade_duration=%g for guild: %s", seconds, guildID)
	return nil
}

func GetFadeOnStop(guildID string) (bool, error) {
	var fadeOnStop int
	err := database.DB.QueryRow(
		`SELECT COALESCE(fade_on_stop, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&fadeOnStop)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get fade_on_stop: %w", err)
	}

	return fadeOnStop == 1, nil
}

func SetFadeOnStop(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, fade_on_stop) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET fade_on_stop = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set fade_on_stop: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetFadeOnStop] Set fade_on_stop=%v for guild: %s", enabled, guildID)
	return nil
}

func GetTrimSilence(guildID string) (bool, error) {
	var trimSilence int
	err := database.DB.QueryRow(
		`SELECT COALESCE(trim_silence, 0) FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&trimSilence)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get trim_silence: %w", err)
	}

	return trimSilence == 1, nil
}

func SetTrimSilence(guildID string, enabled bool) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	value := boolToInt(enabled)
	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, trim_silence) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET trim_silence = ?`,
		guildID, value, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set trim_silence: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetTrimSilence] Set trim_silence=%v for guild: %s", enabled, guildID)
	return nil
}

func GetGuildLanguage(guildID string) (string, error) {
	var lang sql.NullString
	err := database.DB.QueryRow(
		`SELECT language FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&lang)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get guild language: %w", err)
	}

	if !lang.Valid {
		return "", nil
	}
	return lang.String, nil
}

func SetGuildLanguage(guildID, lang string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	var langValue interface{}
	if lang == "" {
		langValue = nil
	} else {
		langValue = lang
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, language) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET language = ?`,
		guildID, langValue, langValue,
	)
	if err != nil {
		return fmt.Errorf("failed to set guild language: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SetGuildLanguage] Set language=%q for guild: %s", lang, guildID)
	return nil
}

func invalidatePrefixCache(guildID string) {
	prefixCacheMux.Lock()
	defer prefixCacheMux.Unlock()
	delete(prefixCache, guildID)
	delete(prefixCacheLoaded, guildID)
}

func GetGuildPrefix(guildID string) (string, error) {
	if guildID == "" {
		return "", nil
	}

	prefixCacheMux.RLock()
	if prefixCacheLoaded[guildID] {
		val := prefixCache[guildID]
		prefixCacheMux.RUnlock()
		return val, nil
	}
	prefixCacheMux.RUnlock()

	var prefix sql.NullString
	err := database.DB.QueryRow(
		`SELECT prefix FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&prefix)

	value := ""
	if err == nil && prefix.Valid {
		value = prefix.String
	} else if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("failed to get guild prefix: %w", err)
	}

	prefixCacheMux.Lock()
	prefixCache[guildID] = value
	prefixCacheLoaded[guildID] = true
	prefixCacheMux.Unlock()

	return value, nil
}

func SetGuildPrefix(guildID, prefix string) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	var prefixValue interface{}
	if prefix == "" {
		prefixValue = nil
	} else {
		prefixValue = prefix
	}

	_, err := database.DB.Exec(
		`INSERT INTO guild_settings (guild_id, prefix) VALUES (?, ?)
		 ON CONFLICT(guild_id) DO UPDATE SET prefix = ?`,
		guildID, prefixValue, prefixValue,
	)
	if err != nil {
		return fmt.Errorf("failed to set guild prefix: %w", err)
	}

	prefixCacheMux.Lock()
	prefixCache[guildID] = prefix
	prefixCacheLoaded[guildID] = true
	prefixCacheMux.Unlock()

	InvalidateCache(guildID)
	logger.Debugf("[SetGuildPrefix] Set prefix=%q for guild: %s", prefix, guildID)
	return nil
}

func SwapSongs(guildID string, pos1, pos2 int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if pos1 < 0 || pos2 < 0 || pos1 >= len(q.Songs) || pos2 >= len(q.Songs) {
		return fmt.Errorf("invalid positions")
	}

	if pos1 == pos2 {
		return nil 
	}

	
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	
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

	
	tempPos := -1

	
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, song1ID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to temp: %w", err)
	}

	
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos1, song2ID)
	if err != nil {
		return fmt.Errorf("failed to move song2 to pos1: %w", err)
	}

	
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		pos2, song1ID)
	if err != nil {
		return fmt.Errorf("failed to move song1 to pos2: %w", err)
	}

	
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[SwapSongs] Swapped songs at positions %d and %d for guild: %s", pos1, pos2, guildID)
	return nil
}

func MoveSong(guildID string, fromPos, toPos int) error {
	lock := acquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	
	q, err := loadQueueFromDB(guildID)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if fromPos < 0 || toPos < 0 || fromPos >= len(q.Songs) || toPos >= len(q.Songs) {
		return fmt.Errorf("invalid positions")
	}

	if fromPos == toPos {
		return nil 
	}

	
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	
	var songID int
	err = tx.QueryRow(
		`SELECT id FROM songs WHERE guild_id = ? AND queue_position = ?`,
		guildID, fromPos,
	).Scan(&songID)
	if err != nil {
		return fmt.Errorf("failed to get song at position %d: %w", fromPos, err)
	}

	
	tempPos := -1
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		tempPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to temp: %w", err)
	}

	
	if fromPos < toPos {
		
		_, err = tx.Exec(
			`UPDATE songs SET queue_position = queue_position - 1
			 WHERE guild_id = ? AND queue_position > ? AND queue_position <= ?`,
			guildID, fromPos, toPos)
	} else {
		
		_, err = tx.Exec(
			`UPDATE songs SET queue_position = queue_position + 1
			 WHERE guild_id = ? AND queue_position >= ? AND queue_position < ?`,
			guildID, toPos, fromPos)
	}
	if err != nil {
		return fmt.Errorf("failed to shift songs: %w", err)
	}

	
	_, err = tx.Exec(
		`UPDATE songs SET queue_position = ? WHERE id = ?`,
		toPos, songID)
	if err != nil {
		return fmt.Errorf("failed to move song to final position: %w", err)
	}

	
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	InvalidateCache(guildID)
	logger.Debugf("[MoveSong] Moved song from position %d to %d for guild: %s", fromPos, toPos, guildID)
	return nil
}

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

	
	InvalidateCache(guildID)
	logger.Debugf("[SaveSeekTime] Saved seek time %dms for song %d in guild: %s", seekTime, songID, guildID)
	return seekTime, nil
}

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

func InvalidateCache(guildID string) {
	cacheMux.Lock()
	defer cacheMux.Unlock()
	delete(cache, guildID)
	logger.Debugf("[Cache] Invalidated cache for guild: %s", guildID)
}
