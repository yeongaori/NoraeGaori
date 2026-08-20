package queue

import (
	"context"
	"sync"
	"time"

	"noraegaori/internal/logger"

	"github.com/bwmarrin/discordgo"
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

	AutoMixStyleVolume string
	AutoMixStyleEQ     string
	AutoMixStyleFilter string
	AutoMixStyleEffect string
	AutoMixStyleLoop   string

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
	logger.Debugf("%s state changed to %s", s.Title, s.getStateName())
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
		logger.Errorf("%s encountered error: %v", s.Title, err)
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
	logger.Debugf("Pre-cache set for %s (%d bytes)", s.Title, len(data))
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
	logger.Debugf("Pre-cache cleared for %s", s.Title)
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
	logger.Debugf("Started playback: %s", s.Title)
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
	logger.Debugf("Paused at %dms: %s", s.SeekTime, s.Title)
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
		ID:                 s.ID,
		GuildID:            s.GuildID,
		URL:                s.URL,
		Title:              s.Title,
		Duration:           s.Duration,
		Thumbnail:          s.Thumbnail,
		RequestedByID:      s.RequestedByID,
		RequestedByTag:     s.RequestedByTag,
		QueuePosition:      s.QueuePosition,
		SeekTime:           s.SeekTime,
		Uploader:           s.Uploader,
		IsLive:             s.IsLive,
		AutoMixStyleVolume: s.AutoMixStyleVolume,
		AutoMixStyleEQ:     s.AutoMixStyleEQ,
		AutoMixStyleFilter: s.AutoMixStyleFilter,
		AutoMixStyleEffect: s.AutoMixStyleEffect,
		AutoMixStyleLoop:   s.AutoMixStyleLoop,
		State:              s.State,
		RetryCount:         s.RetryCount,
		AddedAt:            s.AddedAt,
		StateChangedAt:     s.StateChangedAt,
	}

	return clone
}
