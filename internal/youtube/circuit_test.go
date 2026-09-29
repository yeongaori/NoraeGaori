package youtube

import (
	"errors"
	"testing"
	"time"
)

func useCircuitBreaker(t *testing.T, breaker *circuitBreaker) {
	t.Helper()

	previous := ytCircuitBreaker
	ytCircuitBreaker = breaker
	t.Cleanup(func() { ytCircuitBreaker = previous })
}

var errTooManyRequests = errors.New("WARNING: [youtube] f_2PqUwdijI: Unable to download webpage: HTTP Error 429: Too Many Requests")

func TestCircuitReopensWhenTheTestRequestIsRateLimited(t *testing.T) {
	breaker := &circuitBreaker{state: circuitOpen, lastFailureTime: time.Now().Add(-2 * circuitCooldownPeriod)}
	useCircuitBreaker(t, breaker)

	if err := breaker.canAttempt(); err != nil {
		t.Fatalf("canAttempt returned %v after the cooldown, want the test request allowed", err)
	}
	breaker.recordFailure(errTooManyRequests)

	err := breaker.canAttempt()
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("canAttempt returned %v, want the circuit open again", err)
	}
	if cooldown := RateLimitCooldown(); cooldown < circuitCooldownPeriod-time.Second {
		t.Errorf("got a cooldown of %v, want a fresh full cooldown after the failed test request", cooldown)
	}
}

func TestCircuitClosesWhenTheTestRequestSucceeds(t *testing.T) {
	breaker := &circuitBreaker{state: circuitOpen, lastFailureTime: time.Now().Add(-2 * circuitCooldownPeriod)}
	useCircuitBreaker(t, breaker)

	if err := breaker.canAttempt(); err != nil {
		t.Fatalf("canAttempt returned %v after the cooldown, want the test request allowed", err)
	}
	breaker.recordSuccess()
	breaker.recordFailure(errTooManyRequests)

	if err := breaker.canAttempt(); err != nil {
		t.Errorf("canAttempt returned %v, want a single rate limit after recovery tolerated", err)
	}
}

func TestCircuitIgnoresFailuresThatAreNotRateLimits(t *testing.T) {
	breaker := &circuitBreaker{}
	useCircuitBreaker(t, breaker)

	for range circuitOpenThreshold + 1 {
		breaker.recordFailure(errors.New("ERROR: [youtube] ab429cd: Private video"))
	}

	if err := breaker.canAttempt(); err != nil {
		t.Errorf("canAttempt returned %v, want real errors kept out of the rate limit circuit", err)
	}
}

func TestRateLimitCooldownReportsTheRemainingWait(t *testing.T) {
	cases := map[string]struct {
		breaker  *circuitBreaker
		min, max time.Duration
	}{
		"closed":         {&circuitBreaker{}, 0, 0},
		"open":           {&circuitBreaker{state: circuitOpen, lastFailureTime: time.Now().Add(-10 * time.Second)}, circuitCooldownPeriod - 11*time.Second, circuitCooldownPeriod - 10*time.Second},
		"open, expired":  {&circuitBreaker{state: circuitOpen, lastFailureTime: time.Now().Add(-2 * circuitCooldownPeriod)}, 0, 0},
		"half-open test": {&circuitBreaker{state: circuitHalfOpen, lastFailureTime: time.Now()}, 0, 0},
	}

	for name, check := range cases {
		t.Run(name, func(t *testing.T) {
			useCircuitBreaker(t, check.breaker)

			if got := RateLimitCooldown(); got < check.min || got > check.max {
				t.Errorf("got %v, want between %v and %v", got, check.min, check.max)
			}
		})
	}
}

func TestIsRateLimitErrorRecognisesTheOpenCircuit(t *testing.T) {
	useCircuitBreaker(t, &circuitBreaker{state: circuitOpen, lastFailureTime: time.Now()})

	err := ytCircuitBreaker.canAttempt()
	if !IsRateLimitError(err) {
		t.Errorf("IsRateLimitError(%v) = false, want the open circuit treated as a rate limit", err)
	}
	if IsRateLimitError(nil) {
		t.Error("IsRateLimitError(nil) = true, want false")
	}
	if IsRateLimitError(errors.New("ERROR: [youtube] ab429cd: Private video")) {
		t.Error("a real error was treated as a rate limit")
	}
}
