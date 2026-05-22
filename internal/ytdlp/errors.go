package ytdlp

import "strings"

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
