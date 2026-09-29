package ytdlp_test

import (
	"noraegaori/internal/ytdlp"
)

import "testing"

func TestIsDefinitiveUnavailableError(t *testing.T) {
	cases := map[string]bool{
		"ERROR: Video unavailable":                    true,
		"This is a private video":                     true,
		"Sign in to confirm your age":                 true,
		"Video is not available in your country":      true,
		"members-only content":                        true,
		"This video has been removed by the uploader": true,
		"HTTP Error 429: Too Many Requests":           false,
		"connection refused":                          false,
		"":                                            false,
	}

	for message, want := range cases {
		if got := ytdlp.IsDefinitiveUnavailableError(message); got != want {
			t.Errorf("IsDefinitiveUnavailableError(%q) = %v, want %v", message, got, want)
		}
	}
}

func TestIsNetworkError(t *testing.T) {
	cases := map[string]bool{
		"dial tcp: connection refused":  true,
		"context deadline exceeded":     true,
		"read tcp: connection reset":    true,
		"no such host":                  true,
		"unexpected EOF":                true,
		"write: broken pipe":            true,
		"ERROR: Video unavailable":      false,
		"extractor returned no formats": false,
		"":                              false,
	}

	for message, want := range cases {
		if got := ytdlp.IsNetworkError(message); got != want {
			t.Errorf("IsNetworkError(%q) = %v, want %v", message, got, want)
		}
	}
}

func TestIsRateLimitError(t *testing.T) {
	cases := map[string]bool{
		"WARNING: [youtube] f_2PqUwdijI: Unable to download webpage: HTTP Error 429: Too Many Requests": true,
		"ERROR: [youtube] abc: This content isn't available. Too many requests from this client":        true,
		"The current session has been rate-limited by YouTube for up to an hour":                        true,
		"ERROR: [youtube] ab429cd: Private video":                                                       false,
		"failed to open https://www.youtube.com/watch?v=x429y":                                          false,
		"quota exceeded":               false,
		"dial tcp: connection refused": false,
		"":                             false,
	}

	for message, want := range cases {
		if got := ytdlp.IsRateLimitError(message); got != want {
			t.Errorf("IsRateLimitError(%q) = %v, want %v", message, got, want)
		}
	}
}

func TestErrorClassifiersAreCaseInsensitive(t *testing.T) {
	if !ytdlp.IsDefinitiveUnavailableError("PRIVATE VIDEO") {
		t.Error("IsDefinitiveUnavailableError missed an uppercase pattern")
	}
	if !ytdlp.IsNetworkError("CONNECTION REFUSED") {
		t.Error("IsNetworkError missed an uppercase pattern")
	}
	if !ytdlp.IsRateLimitError("http error 429: too many requests") {
		t.Error("IsRateLimitError missed a lowercase pattern")
	}
}
