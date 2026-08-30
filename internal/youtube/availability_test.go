package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lrstanley/go-ytdlp"
)

func resetAvailabilityState(t *testing.T) {
	t.Helper()

	previousInnertube := checkAvailabilityViaInnertube
	previousYtDlp := runYtDlpAvailability

	t.Cleanup(func() {
		checkAvailabilityViaInnertube = previousInnertube
		runYtDlpAvailability = previousYtDlp
	})

	resetAvailabilityCache()

	ytCircuitBreaker = &circuitBreaker{}
}

func stubInnertubeFailure() {
	checkAvailabilityViaInnertube = func(guildID, url string) (*AvailabilityResult, error) {
		return nil, errors.New("innertube unavailable in tests")
	}
}

func ytdlpResultFor(t *testing.T, payload map[string]any) *ytdlp.Result {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to encode the fake yt-dlp payload: %v", err)
	}

	raw := json.RawMessage(encoded)
	return &ytdlp.Result{
		OutputLogs: []*ytdlp.ResultLog{{JSON: &raw}},
	}
}

func TestCheckVideoAvailabilityUsesInnertubeWhenItSucceeds(t *testing.T) {
	resetAvailabilityState(t)

	want := &AvailabilityResult{Available: true}
	checkAvailabilityViaInnertube = func(guildID, url string) (*AvailabilityResult, error) {
		return want, nil
	}
	runYtDlpAvailability = func(ctx context.Context, url string) (*ytdlp.Result, error) {
		t.Error("yt-dlp was invoked even though innertube succeeded")
		return nil, errors.New("should not run")
	}

	got, err := CheckVideoAvailability("guild", "https://example.invalid/a")
	if err != nil {
		t.Fatalf("CheckVideoAvailability returned %v, want nil", err)
	}
	if got != want {
		t.Errorf("got %+v, want the innertube result", got)
	}
}

func TestCheckVideoAvailabilityServesFromCache(t *testing.T) {
	resetAvailabilityState(t)

	calls := 0
	checkAvailabilityViaInnertube = func(guildID, url string) (*AvailabilityResult, error) {
		calls++
		return &AvailabilityResult{Available: true}, nil
	}

	const url = "https://example.invalid/cached"
	if _, err := CheckVideoAvailability("guild", url); err != nil {
		t.Fatalf("first call returned %v, want nil", err)
	}
	if _, err := CheckVideoAvailability("guild", url); err != nil {
		t.Fatalf("second call returned %v, want nil", err)
	}

	if calls != 1 {
		t.Errorf("the backend ran %d times, want 1 with the second call served from cache", calls)
	}
}

func TestCheckVideoAvailabilityCachesPerGuild(t *testing.T) {
	resetAvailabilityState(t)

	calls := 0
	checkAvailabilityViaInnertube = func(guildID, url string) (*AvailabilityResult, error) {
		calls++
		return &AvailabilityResult{Available: true}, nil
	}

	const url = "https://example.invalid/shared"
	if _, err := CheckVideoAvailability("guildA", url); err != nil {
		t.Fatalf("guildA returned %v, want nil", err)
	}
	if _, err := CheckVideoAvailability("guildB", url); err != nil {
		t.Fatalf("guildB returned %v, want nil", err)
	}

	if calls != 2 {
		t.Errorf("the backend ran %d times, want 2 because the cache key includes the guild", calls)
	}
}

func TestCheckVideoAvailabilityFallsBackToYtDlp(t *testing.T) {
	resetAvailabilityState(t)
	stubInnertubeFailure()

	runYtDlpAvailability = func(ctx context.Context, url string) (*ytdlp.Result, error) {
		return ytdlpResultFor(t, map[string]any{"_type": "video", "id": "abc", "title": "A Song"}), nil
	}

	got, err := CheckVideoAvailability("guild", "https://example.invalid/fallback")
	if err != nil {
		t.Fatalf("CheckVideoAvailability returned %v, want nil", err)
	}
	if got == nil || !got.Available {
		t.Errorf("got %+v, want an available result from the yt-dlp fallback", got)
	}
}

func TestCheckVideoAvailabilityTreatsBlockedErrorsAsUnavailable(t *testing.T) {
	resetAvailabilityState(t)
	stubInnertubeFailure()

	for _, message := range []string{
		"ERROR: Video unavailable",
		"ERROR: Private video",
		"ERROR: This video is a deleted video",
		"ERROR: age-restricted content",
		"ERROR: not available in your country",
	} {
		resetAvailabilityState(t)
		stubInnertubeFailure()

		runYtDlpAvailability = func(ctx context.Context, url string) (*ytdlp.Result, error) {
			return nil, errors.New(message)
		}

		got, err := CheckVideoAvailability("guild", "https://example.invalid/blocked")
		if err != nil {
			t.Errorf("%q: returned error %v, want a definitive unavailable result", message, err)
			continue
		}
		if got == nil || got.Available {
			t.Errorf("%q: got %+v, want Available=false", message, got)
		}
	}
}

func TestCheckVideoAvailabilitySurfacesUnknownErrors(t *testing.T) {
	resetAvailabilityState(t)
	stubInnertubeFailure()

	runYtDlpAvailability = func(ctx context.Context, url string) (*ytdlp.Result, error) {
		return nil, errors.New("some transient network failure")
	}

	got, err := CheckVideoAvailability("guild", "https://example.invalid/unknown")
	if err == nil {
		t.Fatalf("got %+v with no error, want an error for an unclassified failure", got)
	}
}

func TestCheckVideoAvailabilityReportsEmptyInfo(t *testing.T) {
	resetAvailabilityState(t)
	stubInnertubeFailure()

	runYtDlpAvailability = func(ctx context.Context, url string) (*ytdlp.Result, error) {
		return &ytdlp.Result{}, nil
	}

	got, err := CheckVideoAvailability("guild", "https://example.invalid/empty")
	if err != nil {
		t.Fatalf("CheckVideoAvailability returned %v, want nil", err)
	}
	if got == nil || got.Available {
		t.Errorf("got %+v, want an unavailable result when yt-dlp returned no info", got)
	}
}
