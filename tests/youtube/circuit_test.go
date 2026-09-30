package youtube_test

import (
	"errors"
	"testing"
	"time"

	"noraegaori/internal/youtube"
)

func useCircuitBreaker(t *testing.T, breaker *youtube.HookCircuitBreaker) {
	t.Helper()

	previous := *youtube.HookYtCircuitBreaker
	*youtube.HookYtCircuitBreaker = breaker
	t.Cleanup(func() { *youtube.HookYtCircuitBreaker = previous })
}

var errTooManyRequests = errors.New("WARNING: [youtube] f_2PqUwdijI: Unable to download webpage: HTTP Error 429: Too Many Requests")

func TestCircuitReopensWhenTheTestRequestIsRateLimited(t *testing.T) {
	breaker := youtube.HookBuildCircuitBreaker(youtube.HookCircuitBreakerFields{State: youtube.HookCircuitOpen, LastFailureTime: time.Now().Add(-2 * *youtube.HookCircuitCooldownPeriod)})
	useCircuitBreaker(t, breaker)

	if err := breaker.HookCanAttempt(); err != nil {
		t.Fatalf("canAttempt returned %v after the cooldown, want the test request allowed", err)
	}
	breaker.HookRecordFailure(errTooManyRequests)

	err := breaker.HookCanAttempt()
	if !errors.Is(err, youtube.ErrRateLimited) {
		t.Fatalf("canAttempt returned %v, want the circuit open again", err)
	}
	if cooldown := youtube.RateLimitCooldown(); cooldown < *youtube.HookCircuitCooldownPeriod-time.Second {
		t.Errorf("got a cooldown of %v, want a fresh full cooldown after the failed test request", cooldown)
	}
}

func TestCircuitClosesWhenTheTestRequestSucceeds(t *testing.T) {
	breaker := youtube.HookBuildCircuitBreaker(youtube.HookCircuitBreakerFields{State: youtube.HookCircuitOpen, LastFailureTime: time.Now().Add(-2 * *youtube.HookCircuitCooldownPeriod)})
	useCircuitBreaker(t, breaker)

	if err := breaker.HookCanAttempt(); err != nil {
		t.Fatalf("canAttempt returned %v after the cooldown, want the test request allowed", err)
	}
	breaker.HookRecordSuccess()
	breaker.HookRecordFailure(errTooManyRequests)

	if err := breaker.HookCanAttempt(); err != nil {
		t.Errorf("canAttempt returned %v, want a single rate limit after recovery tolerated", err)
	}
}

func TestCircuitIgnoresFailuresThatAreNotRateLimits(t *testing.T) {
	breaker := &youtube.HookCircuitBreaker{}
	useCircuitBreaker(t, breaker)

	for range *youtube.HookCircuitOpenThreshold + 1 {
		breaker.HookRecordFailure(errors.New("ERROR: [youtube] ab429cd: Private video"))
	}

	if err := breaker.HookCanAttempt(); err != nil {
		t.Errorf("canAttempt returned %v, want real errors kept out of the rate limit circuit", err)
	}
}

func TestRateLimitCooldownReportsTheRemainingWait(t *testing.T) {
	cases := map[string]struct {
		breaker  *youtube.HookCircuitBreaker
		min, max time.Duration
	}{
		"closed":         {&youtube.HookCircuitBreaker{}, 0, 0},
		"open":           {youtube.HookBuildCircuitBreaker(youtube.HookCircuitBreakerFields{State: youtube.HookCircuitOpen, LastFailureTime: time.Now().Add(-10 * time.Second)}), *youtube.HookCircuitCooldownPeriod - 11*time.Second, *youtube.HookCircuitCooldownPeriod - 10*time.Second},
		"open, expired":  {youtube.HookBuildCircuitBreaker(youtube.HookCircuitBreakerFields{State: youtube.HookCircuitOpen, LastFailureTime: time.Now().Add(-2 * *youtube.HookCircuitCooldownPeriod)}), 0, 0},
		"half-open test": {youtube.HookBuildCircuitBreaker(youtube.HookCircuitBreakerFields{State: youtube.HookCircuitHalfOpen, LastFailureTime: time.Now()}), 0, 0},
	}

	for name, check := range cases {
		t.Run(name, func(t *testing.T) {
			useCircuitBreaker(t, check.breaker)

			if got := youtube.RateLimitCooldown(); got < check.min || got > check.max {
				t.Errorf("got %v, want between %v and %v", got, check.min, check.max)
			}
		})
	}
}

func TestIsRateLimitErrorRecognisesTheOpenCircuit(t *testing.T) {
	useCircuitBreaker(t, youtube.HookBuildCircuitBreaker(youtube.HookCircuitBreakerFields{State: youtube.HookCircuitOpen, LastFailureTime: time.Now()}))

	err := (*youtube.HookYtCircuitBreaker).HookCanAttempt()
	if !youtube.IsRateLimitError(err) {
		t.Errorf("IsRateLimitError(%v) = false, want the open circuit treated as a rate limit", err)
	}
	if youtube.IsRateLimitError(nil) {
		t.Error("IsRateLimitError(nil) = true, want false")
	}
	if youtube.IsRateLimitError(errors.New("ERROR: [youtube] ab429cd: Private video")) {
		t.Error("a real error was treated as a rate limit")
	}
}
