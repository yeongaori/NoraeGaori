package youtube

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"noraegaori/internal/messages"
	"noraegaori/pkg/logger"
)

type VideoError struct {
	Message string
	Reason  string
}

func (e *VideoError) Error() string {
	return e.Message
}

func parseYtDlpError(guildID string, err error) error {
	if err == nil {
		return nil
	}

	errorLower := strings.ToLower(err.Error())

	yt := messages.T(guildID).YouTube
	errorMappings := []struct {
		patterns []string
		message  string
	}{
		{
			patterns: []string{"sign in to confirm your age", "age-restricted", "age restricted"},
			message:  yt.ErrorAgeRestricted,
		},
		{
			patterns: []string{"not available in your country", "video is not available", "this video is not available"},
			message:  yt.ErrorGeoRestricted,
		},
		{
			patterns: []string{"private video", "[private video]"},
			message:  yt.ErrorPrivateVideo,
		},
		{
			patterns: []string{"deleted video", "[deleted video]"},
			message:  yt.ErrorDeletedVideo,
		},
		{
			patterns: []string{"video unavailable"},
			message:  yt.ErrorUnavailable,
		},
		{
			patterns: []string{"members-only", "members only", "join this channel"},
			message:  yt.ErrorMembersOnly,
		},
		{
			patterns: []string{"premium"},
			message:  yt.ErrorPremiumOnly,
		},
		{
			patterns: []string{"copyright"},
			message:  yt.ErrorCopyright,
		},
	}

	for _, mapping := range errorMappings {
		for _, pattern := range mapping.patterns {
			if strings.Contains(errorLower, pattern) {
				return &VideoError{
					Message: mapping.message,
					Reason:  err.Error(),
				}
			}
		}
	}

	return err
}

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

type circuitBreaker struct {
	state            circuitState
	failureCount     int
	lastFailureTime  time.Time
	consecutiveFails int
	mu               sync.RWMutex
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "too many requests") ||
		strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "quota exceeded")
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == circuitHalfOpen {
		logger.Info("Test request succeeded, closing circuit")
		cb.state = circuitClosed
	}
	cb.consecutiveFails = 0
}

func (cb *circuitBreaker) recordFailure(err error) {
	if !isRateLimitError(err) {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFails++
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.consecutiveFails >= circuitOpenThreshold && cb.state == circuitClosed {
		logger.Warnf("Opening circuit after %d consecutive rate limit errors", cb.consecutiveFails)
		cb.state = circuitOpen
	}
}

func (cb *circuitBreaker) canAttempt() error {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailureTime
	cb.mu.RUnlock()

	switch state {
	case circuitClosed:
		return nil
	case circuitOpen:

		if time.Since(lastFailure) > circuitCooldownPeriod {
			cb.mu.Lock()
			cb.state = circuitHalfOpen
			cb.mu.Unlock()
			logger.Info("Cooldown complete, entering half-open state (testing)")
			return nil
		}
		return fmt.Errorf("YouTube rate limit exceeded, please wait %v before trying again",
			circuitCooldownPeriod-time.Since(lastFailure))
	case circuitHalfOpen:
		return nil
	}
	return nil
}
