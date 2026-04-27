package ytdlp

import "strings"

// IsDefinitiveUnavailableError checks if an error indicates the video is truly unavailable
// (geo-restricted, age-restricted, private, deleted) vs. a potential false negative.
// These errors are NOT caused by a broken yt-dlp binary.
func IsDefinitiveUnavailableError(errorMsg string) bool {
	errorLower := strings.ToLower(errorMsg)
	definitivePatterns := []string{
		"video unavailable",
		"not available",
		"private video",
		"deleted video",
		"age-restricted",
		"age restricted",
		"sign in to confirm your age",
		"login required",
		"not available in your country",
		"geo",
		"members-only",
		"members only",
		"premium",
		"copyright",
		"blocked",
		"removed by the uploader",
		"account associated with this video has been terminated",
	}
	for _, pattern := range definitivePatterns {
		if strings.Contains(errorLower, pattern) {
			return true
		}
	}
	return false
}

// IsNetworkError checks if an error is a network/timeout issue rather than
// a yt-dlp binary problem. These errors should not count toward version rollback.
func IsNetworkError(errorMsg string) bool {
	errorLower := strings.ToLower(errorMsg)
	networkPatterns := []string{
		"timed out",
		"connection reset",
		"network unreachable",
		"no route to host",
		"connection refused",
		"deadline exceeded",
		"context canceled",
		"dns lookup",
		"no such host",
		"eof",
		"broken pipe",
	}
	for _, pattern := range networkPatterns {
		if strings.Contains(errorLower, pattern) {
			return true
		}
	}
	return false
}
