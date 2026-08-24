package queue

import (
	"sync"
	"time"

	"noraegaori/internal/logger"
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
