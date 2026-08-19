package ytdlp

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
		if got := IsDefinitiveUnavailableError(message); got != want {
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
		if got := IsNetworkError(message); got != want {
			t.Errorf("IsNetworkError(%q) = %v, want %v", message, got, want)
		}
	}
}

func TestErrorClassifiersAreCaseInsensitive(t *testing.T) {
	if !IsDefinitiveUnavailableError("PRIVATE VIDEO") {
		t.Error("IsDefinitiveUnavailableError missed an uppercase pattern")
	}
	if !IsNetworkError("CONNECTION REFUSED") {
		t.Error("IsNetworkError missed an uppercase pattern")
	}
}
