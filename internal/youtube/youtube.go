package youtube

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lrstanley/go-ytdlp"
	"github.com/ppalone/ytsearch"
	"noraegaori/internal/messages"
	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"
)

// Song represents a YouTube song
type Song struct {
	URL           string
	Title         string
	Duration      string
	Thumbnail     string
	Uploader      string
	IsLive        bool
	RequestedBy   string
	RequestedByID string
}

// SearchResult is an alias for ytsearch.VideoInfo for easier use
type SearchResult = ytsearch.VideoInfo

// PlaylistInfo represents information about a YouTube playlist
type PlaylistInfo struct {
	ID            string
	Title         string
	URL           string
	ThumbnailURL  string
	VideoCount    int
	Videos        []*Song
}

// URLType represents the type of YouTube URL
type URLType string

const (
	URLTypePurePlaylist      URLType = "pure_playlist"
	URLTypeVideoWithPlaylist URLType = "video_with_playlist"
	URLTypeVideoOnly         URLType = "video_only"
)

// URLAnalysis contains the result of analyzing a YouTube URL
type URLAnalysis struct {
	Type       URLType
	VideoID    string
	PlaylistID string
}

// AvailabilityResult represents the result of checking video availability
type AvailabilityResult struct {
	Available bool
	Error     string
	IsLive    bool
}

// cachedAvailability represents a cached availability check result
type cachedAvailability struct {
	result    *AvailabilityResult
	timestamp time.Time
}

// VideoError represents a specific error type for video fetching
type VideoError struct {
	Message string // User-friendly error message
	Reason  string // Technical reason
}

func (e *VideoError) Error() string {
	return e.Message
}

// parseYtDlpError parses yt-dlp error messages and returns user-friendly error messages
func parseYtDlpError(err error) error {
	if err == nil {
		return nil
	}

	errorLower := strings.ToLower(err.Error())

	// Map of patterns to user-friendly localized messages
	yt := messages.T().YouTube
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

	// If no specific pattern matched, return the original error
	return err
}

// Circuit breaker state
type circuitState int

const (
	circuitClosed   circuitState = iota // Normal operation
	circuitOpen                         // Failing fast due to rate limits
	circuitHalfOpen                     // Testing if service recovered
)

// circuitBreaker tracks rate limit failures for fail-fast behavior
type circuitBreaker struct {
	state            circuitState
	failureCount     int
	lastFailureTime  time.Time
	consecutiveFails int
	mu               sync.RWMutex
}

var (
	// YouTube URL regex patterns - matches youtube.com, youtu.be, and music.youtube.com
	youtubeRegex = regexp.MustCompile(`^(https?://)?(www\.)?(music\.youtube\.com|youtube\.com|youtu\.be)/.+$`)
	searchClient *ytsearch.Client

	// Availability check cache
	availabilityCache = &sync.Map{}
	cacheTTL          = 10 * time.Minute // Cache results for 10 minutes

	// Circuit breaker for rate limiting
	ytCircuitBreaker = &circuitBreaker{
		state: circuitClosed,
	}
	circuitOpenThreshold   = 5               // Open circuit after 5 consecutive rate limit errors
	circuitCooldownPeriod  = 60 * time.Second // Wait 60s before trying again
)

// isRateLimitError checks if an error is a rate limit error
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

// recordSuccess records a successful operation and may close the circuit
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == circuitHalfOpen {
		logger.Info("[CircuitBreaker] Test request succeeded, closing circuit")
		cb.state = circuitClosed
	}
	cb.consecutiveFails = 0
}

// recordFailure records a failed operation and may open the circuit
func (cb *circuitBreaker) recordFailure(err error) {
	if !isRateLimitError(err) {
		return // Only track rate limit errors
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFails++
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.consecutiveFails >= circuitOpenThreshold && cb.state == circuitClosed {
		logger.Warnf("[CircuitBreaker] Opening circuit after %d consecutive rate limit errors", cb.consecutiveFails)
		cb.state = circuitOpen
	}
}

// canAttempt checks if a request should be allowed
func (cb *circuitBreaker) canAttempt() error {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailureTime
	cb.mu.RUnlock()

	switch state {
	case circuitClosed:
		return nil // Normal operation
	case circuitOpen:
		// Check if cooldown period has passed
		if time.Since(lastFailure) > circuitCooldownPeriod {
			cb.mu.Lock()
			cb.state = circuitHalfOpen
			cb.mu.Unlock()
			logger.Info("[CircuitBreaker] Cooldown complete, entering half-open state (testing)")
			return nil
		}
		return fmt.Errorf("YouTube rate limit exceeded, please wait %v before trying again",
			circuitCooldownPeriod-time.Since(lastFailure))
	case circuitHalfOpen:
		return nil // Allow test request
	}
	return nil
}

// Initialize sets up the YouTube client
func Initialize() error {
	// Initialize search client with default http client
	searchClient = ytsearch.NewClient(nil)

	return nil
}

// IsYouTubeURL checks if a string is a YouTube URL
func IsYouTubeURL(query string) bool {
	return youtubeRegex.MatchString(query)
}

// Search searches YouTube for a query
func Search(query string, requesterName, requesterID string) (*Song, error) {
	if IsYouTubeURL(query) {
		// Get video info from URL
		return GetVideoInfo(query, requesterName, requesterID)
	}

	// Search YouTube
	logger.Debugf("Searching YouTube for: %s", query)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := searchClient.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search YouTube: %w", err)
	}

	if len(response.Results) == 0 {
		return nil, fmt.Errorf("no results found for: %s", query)
	}

	video := response.Results[0]
	videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", video.VideoID)

	// Get detailed info for the video
	return GetVideoInfo(videoURL, requesterName, requesterID)
}

// SearchMultiple searches YouTube and returns multiple results
func SearchMultiple(query string, limit int) ([]SearchResult, error) {
	logger.Debugf("Searching YouTube for multiple results: %s", query)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := searchClient.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search YouTube: %w", err)
	}

	if len(response.Results) == 0 {
		return nil, fmt.Errorf("no results found")
	}

	// Return up to limit results
	if len(response.Results) > limit {
		return response.Results[:limit], nil
	}

	return response.Results, nil
}

// CheckVideoAvailability checks if a video is available and not restricted
// This matches the NodeJS implementation's comprehensive checking
// Uses fast Innertube API (100-300ms) with fallback to yt-dlp (2300ms) on failure
func CheckVideoAvailability(url string) (*AvailabilityResult, error) {
	// Check cache first
	if cached, ok := availabilityCache.Load(url); ok {
		cachedEntry := cached.(*cachedAvailability)
		if time.Since(cachedEntry.timestamp) < cacheTTL {
			logger.Debugf("[Availability] Cache hit for: %s (age: %v)", url, time.Since(cachedEntry.timestamp))
			return cachedEntry.result, nil
		}
		// Cache expired, remove it
		availabilityCache.Delete(url)
		logger.Debugf("[Availability] Cache expired for: %s", url)
	}

	// Check circuit breaker before making request
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[Availability] Circuit breaker open: %v", err)
		return nil, err
	}

	startTime := time.Now()
	logger.Debugf("[Availability] Starting check for: %s", url)

	client := getInnertubeClient()
	availResult, innertubeErr := client.CheckVideoAvailability(url)

	if innertubeErr != nil {
		// Innertube failed, fallback to yt-dlp
		logger.Warnf("[Availability] Innertube failed, falling back to yt-dlp: %v", innertubeErr)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Create ytdlp command with flags to skip download and get flat playlist info
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			FlatPlaylist().
			JsRuntimes("node").
			SkipDownload()

		// Run yt-dlp to get video information
		result, err := cmd.Run(ctx, url)
		if err != nil {
			// Record failure in circuit breaker
			ytCircuitBreaker.recordFailure(err)
			checkTime := time.Since(startTime)
			logger.Debugf("[Availability] yt-dlp error after %v: %v", checkTime, err)

			// Parse error message for specific restrictions
			errorMsg := strings.ToLower(err.Error())
			if strings.Contains(errorMsg, "video unavailable") ||
				strings.Contains(errorMsg, "private video") ||
				strings.Contains(errorMsg, "deleted video") ||
				strings.Contains(errorMsg, "age-restricted") ||
				strings.Contains(errorMsg, "not available in your country") ||
				strings.Contains(errorMsg, "geo") {
				logger.Infof("[Availability] Video blocked by error: %v (%v)", err, checkTime)
				unavailResult := &AvailabilityResult{
					Available: false,
					Error:     err.Error(),
				}
				// Cache unavailable results
				availabilityCache.Store(url, &cachedAvailability{
					result:    unavailResult,
					timestamp: time.Now(),
				})
				return unavailResult, nil
			}

			// Other errors (network, etc.) should be returned as errors
			return nil, fmt.Errorf("failed to check availability: %w", err)
		}

		// Parse extracted info
		infos, err := result.GetExtractedInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to parse video info: %w", err)
		}

		if len(infos) == 0 {
			return &AvailabilityResult{
				Available: false,
				Error:     "no video info returned",
			}, nil
		}

		info := infos[0]
		checkTime := time.Since(startTime)
		logger.Debugf("[Availability] yt-dlp info fetched in %v for: %s", checkTime, getStringValue(info.Title))

		unavailableReasons := []string{}

		// Check age restriction
		if info.AgeLimit != nil && *info.AgeLimit > 0 {
			unavailableReasons = append(unavailableReasons, messages.T().YouTube.ErrorAgeVerification)
			logger.Debugf("[Availability] age_limit: %g", *info.AgeLimit)
		}

		// Check if it's a live stream
		isLive := info.IsLive != nil && *info.IsLive ||
			(info.LiveStatus != nil && (*info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive ||
				*info.LiveStatus == ytdlp.ExtractedLiveStatusIsUpcoming))
		if isLive {
			logger.Infof("[Availability] \"%s\" is a LIVE stream", getStringValue(info.Title))
		}

		// Check availability status
		if info.Availability != nil {
			availability := strings.ToLower(string(*info.Availability))
			logger.Debugf("[Availability] availability: %s", availability)
			// Allow "public" and "unlisted" videos, block "private" and restricted content
			if availability != "public" && availability != "unlisted" {
				unavailableReasons = append(unavailableReasons, messages.T().YouTube.ErrorRegionRestricted)
			}
		}

		// Check if video is private or deleted from title
		if info.Title != nil {
			title := strings.ToLower(*info.Title)
			if strings.Contains(title, "[private video]") ||
				strings.Contains(title, "[deleted video]") ||
				strings.Contains(title, "private video") ||
				strings.Contains(title, "deleted video") {
				unavailableReasons = append(unavailableReasons, messages.T().YouTube.ErrorPrivateOrDeleted)
				logger.Debugf("[Availability] title_indicates_unavailable: true")
			}
		}

		if len(unavailableReasons) > 0 {
			errorMsg := strings.Join(unavailableReasons, ", ")
			logger.Infof("[Availability] \"%s\" unavailable: %s (%v)", getStringValue(info.Title), errorMsg, checkTime)
			availResult = &AvailabilityResult{
				Available: false,
				Error:     errorMsg,
				IsLive:    isLive,
			}
		} else {
			logger.Infof("[Availability] \"%s\" is available (%v)", getStringValue(info.Title), checkTime)
			availResult = &AvailabilityResult{
				Available: true,
				IsLive:    isLive,
			}
		}
	}

	// Store in cache
	availabilityCache.Store(url, &cachedAvailability{
		result:    availResult,
		timestamp: time.Now(),
	})
	logger.Debugf("[Availability] Cached result for: %s", url)

	// Record success in circuit breaker
	ytCircuitBreaker.recordSuccess()

	return availResult, nil
}

// retryWithBackoff executes a function with exponential backoff retry logic
// Reduced retries for faster failure recovery: 2^retry * 1000ms, max 10 seconds, max 3 retries
func retryWithBackoff(operation func() error, operationName string) error {
	const maxRetries = 3
	const baseDelay = 1000 * time.Millisecond
	const maxDelay = 10 * time.Second

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := operation()
		if err == nil {
			if attempt > 0 {
				logger.Infof("[Retry] %s succeeded after %d attempts", operationName, attempt+1)
			}
			return nil
		}

		lastErr = err

		// Check if error is retryable (network errors, rate limits)
		errorMsg := strings.ToLower(err.Error())
		isRetryable := strings.Contains(errorMsg, "network") ||
			strings.Contains(errorMsg, "timeout") ||
			strings.Contains(errorMsg, "rate limit") ||
			strings.Contains(errorMsg, "too many requests") ||
			strings.Contains(errorMsg, "connection") ||
			strings.Contains(errorMsg, "temporary failure")

		if !isRetryable {
			// Don't retry non-network errors
			return err
		}

		if attempt < maxRetries-1 {
			// Calculate backoff delay: 2^attempt * 1000ms, max 30s
			delay := time.Duration(1<<uint(attempt)) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			logger.Warnf("[Retry] %s failed (attempt %d/%d): %v, retrying in %v",
				operationName, attempt+1, maxRetries, err, delay)
			time.Sleep(delay)
		}
	}

	logger.Errorf("[Retry] %s failed after %d attempts: %v", operationName, maxRetries, lastErr)
	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// GetVideoInfo retrieves detailed information about a YouTube video
// Uses fast Innertube API (300-500ms) to get both availability and video info in a single call
// This eliminates the previous double-fetch pattern (CheckVideoAvailability + GetVideoInfo)
// Falls back to yt-dlp (4-6s) if Innertube fails
func GetVideoInfo(url, requesterName, requesterID string) (*Song, error) {
	logger.Debugf("Fetching video info for: %s", url)

	client := getInnertubeClient()
	song, innertubeErr := client.GetVideoInfo(url, requesterName, requesterID)

	if innertubeErr == nil {
		// Success with Innertube
		return song, nil
	}

	// Innertube failed, fallback to yt-dlp method
	logger.Warnf("[GetVideoInfo] Innertube failed, falling back to yt-dlp: %v", innertubeErr)

	// First, check video availability using the comprehensive check
	availability, err := CheckVideoAvailability(url)
	if err != nil {
		logger.Warnf("Failed to check video availability (continuing anyway): %v", err)
	} else if !availability.Available {
		// Return parsed error message for unavailable videos
		errMsg := fmt.Errorf("video is not available: %s", availability.Error)
		return nil, parseYtDlpError(errMsg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result *ytdlp.Result

	// Retry video info fetching with exponential backoff
	retryErr := retryWithBackoff(func() error {
		// Create ytdlp command
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			NoPlaylist().
			JsRuntimes("node").
			Format("bestaudio/best")

		// Run yt-dlp to get video information
		var err error
		result, err = cmd.Run(ctx, url)
		if err != nil {
			return fmt.Errorf("failed to get video info: %w", err)
		}
		return nil
	}, "GetVideoInfo")

	if retryErr != nil {
		// Parse yt-dlp error to provide specific user-friendly message
		return nil, parseYtDlpError(retryErr)
	}

	// Parse extracted info
	infos, err := result.GetExtractedInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to parse video info: %w", err)
	}

	if len(infos) == 0 {
		return nil, fmt.Errorf("no video info returned")
	}

	info := infos[0]

	// Parse duration
	var duration string
	var isLive bool

	if info.LiveStatus != nil && (*info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive ||
	   *info.LiveStatus == ytdlp.ExtractedLiveStatusIsUpcoming) {
		duration = "🔴 LIVE"
		isLive = true
	} else if info.Duration != nil && *info.Duration > 0 {
		duration = formatDuration(int(*info.Duration))
	} else {
		duration = "Unknown"
	}

	// Get thumbnail
	thumbnail := ""
	if len(info.Thumbnails) > 0 {
		// Get the best quality thumbnail
		thumbnail = info.Thumbnails[len(info.Thumbnails)-1].URL
	}

	// Get uploader
	uploader := "Unknown"
	if info.Uploader != nil && *info.Uploader != "" {
		uploader = *info.Uploader
	} else if info.Channel != nil && *info.Channel != "" {
		uploader = *info.Channel
	}

	// Get title
	title := "Unknown"
	if info.Title != nil {
		title = *info.Title
	}

	song = &Song{
		URL:           url,
		Title:         title,
		Duration:      duration,
		Thumbnail:     thumbnail,
		Uploader:      uploader,
		IsLive:        isLive,
		RequestedBy:   requesterName,
		RequestedByID: requesterID,
	}

	logger.Debugf("Retrieved video: %s (%s)", song.Title, song.Duration)
	return song, nil
}

// GetOptimalAudioFormat returns the optimal yt-dlp audio format string based on voice channel bitrate
// Matches NodeJS modules/music/player.js:161-205 behavior
func GetOptimalAudioFormat(bitrate int) string {
	if bitrate <= 0 {
		// Default to best audio if bitrate is unknown
		logger.Debugf("[AudioFormat] Voice channel bitrate unknown, using bestaudio")
		return "bestaudio/best"
	}

	// Discord voice channel bitrates (in bps):
	// - 8 kbps (8000): Low quality
	// - 32 kbps (32000): Normal
	// - 64 kbps (64000): Medium (default for most servers)
	// - 96 kbps (96000): High (Nitro servers)
	// - 128 kbps (128000): Very High (Nitro Level 2)
	// - 384 kbps (384000): Max (Nitro Level 3, Stage channels)

	bitrateKbps := bitrate / 1000
	logger.Debugf("[AudioFormat] Voice channel bitrate: %d kbps", bitrateKbps)

	// Select appropriate audio quality to avoid wasting bandwidth and memory
	// Format selection uses yt-dlp's fallback chain (left to right):
	// Try specific bitrate → Try lower bitrates → Use best available
	if bitrate <= 32000 {
		// Low quality channels: Try 32k, then any lower, then best
		logger.Debugf("[AudioFormat] Using low quality audio (≤32k)")
		return "bestaudio[abr<=32]/bestaudio[abr<=48]/bestaudio[abr<=64]/bestaudio/best"
	} else if bitrate <= 64000 {
		// Normal/Medium channels: Try 64k, then lower options, then best
		logger.Debugf("[AudioFormat] Using medium quality audio (≤64k)")
		return "bestaudio[abr<=64]/bestaudio[abr<=96]/bestaudio/best"
	} else if bitrate <= 96000 {
		// High quality channels: Try 96k, then 128k fallback, then best
		logger.Debugf("[AudioFormat] Using high quality audio (≤96k)")
		return "bestaudio[abr<=96]/bestaudio[abr<=128]/bestaudio/best"
	} else if bitrate <= 128000 {
		// Very high quality: Try 128k, then 160k fallback, then best
		logger.Debugf("[AudioFormat] Using very high quality audio (≤128k)")
		return "bestaudio[abr<=128]/bestaudio[abr<=160]/bestaudio/best"
	} else {
		// Max quality channels (384 kbps): Use best available without limit
		logger.Debugf("[AudioFormat] Using maximum quality audio")
		return "bestaudio/best"
	}
}

// GetStreamURL retrieves the direct stream URL for a video
// bitrate is the voice channel bitrate in bps (pass 0 for default best quality)
func GetStreamURL(url string, sponsorBlock bool, bitrate int) (string, error) {
	// Check circuit breaker before making request
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[GetStreamURL] Circuit breaker open: %v", err)
		return "", err
	}

	audioFormat := GetOptimalAudioFormat(bitrate)
	logger.Debugf("Getting stream URL for: %s (SponsorBlock: %v, Format: %s)", url, sponsorBlock, audioFormat)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var streamURL string

	// Retry stream URL fetching with exponential backoff
	retryErr := retryWithBackoff(func() error {
		// Create ytdlp command with optimal audio format
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			GetURL().
			NoPlaylist().
			JsRuntimes("node").
			Format(audioFormat)

		// Add SponsorBlock if enabled
		if sponsorBlock {
			cmd = cmd.SponsorblockMark("all").
				SponsorblockRemove("sponsor,selfpromo,interaction,intro,outro")
		}

		// Run yt-dlp
		logger.Debugf("[GetStreamURL] Running yt-dlp command for: %s", url)
		result, err := cmd.Run(ctx, url)
		if err != nil {
			logger.Errorf("[GetStreamURL] yt-dlp failed: %v", err)
			ytCircuitBreaker.recordFailure(err) // Record failure for circuit breaker
			return fmt.Errorf("failed to get stream URL: %w", err)
		}
		logger.Debugf("[GetStreamURL] yt-dlp completed successfully")

		streamURL = result.Stdout
		if streamURL == "" {
			logger.Errorf("[GetStreamURL] Empty stream URL returned")
			return fmt.Errorf("empty stream URL returned")
		}

		logger.Debugf("[GetStreamURL] Got stream URL (length: %d)", len(streamURL))
		return nil
	}, "GetStreamURL")

	if retryErr != nil {
		return "", retryErr
	}

	// Record success in circuit breaker
	ytCircuitBreaker.recordSuccess()

	return streamURL, nil
}

// formatDuration converts seconds to HH:MM:SS or MM:SS format
func formatDuration(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// ParseDurationToSeconds converts duration string (HH:MM:SS or MM:SS) to seconds
func ParseDurationToSeconds(duration string) int {
	if duration == "" || duration == "Unknown" || duration == "🔴 LIVE" {
		return 0
	}

	var hours, minutes, seconds int
	if _, err := fmt.Sscanf(duration, "%d:%d:%d", &hours, &minutes, &seconds); err == nil {
		return hours*3600 + minutes*60 + seconds
	}
	if _, err := fmt.Sscanf(duration, "%d:%d", &minutes, &seconds); err == nil {
		return minutes*60 + seconds
	}

	return 0
}

// IsLiveStreamActive checks if a live stream is still active
// Uses fast Innertube API (100-200ms) with fallback to yt-dlp (2-3s)
func IsLiveStreamActive(url string) (bool, error) {
	logger.Debugf("Checking if live stream is active: %s", url)

	client := getInnertubeClient()
	available, isLive, err := client.CheckAvailability(url)

	if err == nil {
		// Success with Innertube
		if !available {
			logger.Debugf("Live stream is not available: %s", url)
			return false, nil
		}
		if isLive {
			logger.Debugf("Live stream is active: %s", url)
			return true, nil
		}
		logger.Debugf("Live stream is not active (not live): %s", url)
		return false, nil
	}

	// Innertube failed, fallback to yt-dlp
	logger.Warnf("[IsLiveStreamActive] Innertube failed, falling back to yt-dlp: %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create ytdlp command
	cmd := ytdlp.New().
		SetExecutable(ytdlpUpdater.GetBinaryPath()).
		DumpJSON().
		NoPlaylist().
		JsRuntimes("node").
		Format("bestaudio/best")

	// Run yt-dlp to get video information
	result, ytdlpErr := cmd.Run(ctx, url)
	if ytdlpErr != nil {
		return false, fmt.Errorf("failed to get video info: %w", ytdlpErr)
	}

	// Parse extracted info
	infos, parseErr := result.GetExtractedInfo()
	if parseErr != nil {
		return false, fmt.Errorf("failed to parse video info: %w", parseErr)
	}

	if len(infos) == 0 {
		return false, fmt.Errorf("no video info returned")
	}

	info := infos[0]

	// Check if live stream is active
	if info.LiveStatus != nil && *info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive {
		logger.Debugf("Live stream is active: %s", url)
		return true, nil
	}

	logger.Debugf("Live stream is not active: %s", url)
	return false, nil
}

// CheckIfLive checks if a YouTube URL is currently a live stream
// Alias for IsLiveStreamActive for compatibility
func CheckIfLive(url string) (bool, error) {
	return IsLiveStreamActive(url)
}

// CheckIfLiveStreamEnded checks if a previously live stream has ended
// This is used for resume command to skip ended live streams
// Matches NodeJS resume.js:85-98 behavior
func CheckIfLiveStreamEnded(url string) (bool, error) {
	logger.Debugf("Checking if live stream has ended: %s", url)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var info *ytdlp.ExtractedInfo
	var infos []*ytdlp.ExtractedInfo

	// Retry with exponential backoff
	retryErr := retryWithBackoff(func() error {
		// Create ytdlp command with FlatPlaylist for fast checking
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			FlatPlaylist().
			JsRuntimes("node").
			NoPlaylist()

		// Run yt-dlp to get video information
		result, err := cmd.Run(ctx, url)
		if err != nil {
			return fmt.Errorf("failed to get video info: %w", err)
		}

		// Parse extracted info
		infos, err = result.GetExtractedInfo()
		if err != nil {
			return fmt.Errorf("failed to parse video info: %w", err)
		}

		if len(infos) == 0 {
			return fmt.Errorf("no video info returned")
		}

		info = infos[0]
		return nil
	}, "CheckIfLiveStreamEnded")

	if retryErr != nil {
		return false, retryErr
	}

	// Check if the stream is still live
	// Matches NodeJS: info.is_live === true || info.live_status === 'is_live'
	isStillLive := info.IsLive != nil && *info.IsLive ||
		(info.LiveStatus != nil && *info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive)

	if !isStillLive {
		logger.Infof("Live stream has ended: %s", url)
		return true, nil // Stream has ended
	}

	logger.Debugf("Live stream is still active: %s", url)
	return false, nil // Stream is still live
}

// AnalyzeYouTubeURL analyzes a YouTube URL to determine its type
func AnalyzeYouTubeURL(urlStr string) *URLAnalysis {
	// Add https if missing
	if !regexp.MustCompile(`^https?://`).MatchString(urlStr) {
		urlStr = "https://" + urlStr
	}

	// Check if it's a pure playlist URL
	playlistRegex := regexp.MustCompile(`youtube\.com/playlist\?list=([a-zA-Z0-9_-]+)`)
	if matches := playlistRegex.FindStringSubmatch(urlStr); len(matches) > 1 {
		return &URLAnalysis{
			Type:       URLTypePurePlaylist,
			PlaylistID: matches[1],
		}
	}

	// Check if it's a video URL with playlist
	videoWithListRegex := regexp.MustCompile(`[?&]v=([a-zA-Z0-9_-]+).*[?&]list=([a-zA-Z0-9_-]+)`)
	if matches := videoWithListRegex.FindStringSubmatch(urlStr); len(matches) > 2 {
		return &URLAnalysis{
			Type:       URLTypeVideoWithPlaylist,
			VideoID:    matches[1],
			PlaylistID: matches[2],
		}
	}

	// Check for youtu.be format with playlist
	youtuBeRegex := regexp.MustCompile(`youtu\.be/([a-zA-Z0-9_-]+).*[?&]list=([a-zA-Z0-9_-]+)`)
	if matches := youtuBeRegex.FindStringSubmatch(urlStr); len(matches) > 2 {
		return &URLAnalysis{
			Type:       URLTypeVideoWithPlaylist,
			VideoID:    matches[1],
			PlaylistID: matches[2],
		}
	}

	// Check for regular video URL (watch?v=)
	watchRegex := regexp.MustCompile(`[?&]v=([a-zA-Z0-9_-]+)`)
	if matches := watchRegex.FindStringSubmatch(urlStr); len(matches) > 1 {
		return &URLAnalysis{
			Type:    URLTypeVideoOnly,
			VideoID: matches[1],
		}
	}

	// Check for youtu.be short format
	youtuBeShortRegex := regexp.MustCompile(`youtu\.be/([a-zA-Z0-9_-]+)`)
	if matches := youtuBeShortRegex.FindStringSubmatch(urlStr); len(matches) > 1 {
		return &URLAnalysis{
			Type:    URLTypeVideoOnly,
			VideoID: matches[1],
		}
	}

	return &URLAnalysis{
		Type: URLTypeVideoOnly,
	}
}

// GetPlaylistInfo retrieves information about a YouTube playlist
func GetPlaylistInfo(url, requesterName, requesterID string) (*PlaylistInfo, error) {
	logger.Debugf("Fetching playlist info for: %s", url)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create ytdlp command for playlist
	// Use FlatPlaylist() for fast fetching - with this flag, yt-dlp returns:
	// - infos[0]: playlist metadata
	// - infos[1:]: individual video entries with basic info (fast!)
	// Use IgnoreErrors() to skip unavailable videos instead of failing the entire playlist
	cmd := ytdlp.New().
		SetExecutable(ytdlpUpdater.GetBinaryPath()).
		ExtractorArgs("youtube:lang=" + messages.Lang()).
		DumpJSON().
		FlatPlaylist().
		JsRuntimes("node").
		IgnoreErrors()

	// Run yt-dlp to get playlist information
	result, err := cmd.Run(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to get playlist info: %w", err)
	}

	// Parse extracted info
	infos, err := result.GetExtractedInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to parse playlist info: %w", err)
	}

	if len(infos) == 0 {
		return nil, fmt.Errorf("no playlist info returned")
	}

	info := infos[0]

	// Check if it's a playlist by checking playlist count or multiple infos
	// With FlatPlaylist(), videos are in infos[1:], not info.Entries
	if info.PlaylistCount == nil && len(infos) <= 1 {
		return nil, fmt.Errorf("URL is not a playlist (no entries found)")
	}

	// With FlatPlaylist(), yt-dlp behavior varies:
	// - Sometimes infos[0] is playlist metadata only, infos[1:] are videos
	// - Sometimes infos[0] is the first video (with playlist metadata), infos[1:] are remaining videos
	// We detect this by checking if infos[0] has a Duration field (indicates it's a video)
	var videoInfos []*ytdlp.ExtractedInfo
	var playlistTitle string

	if info.Duration != nil {
		// infos[0] is a video, include all infos
		// Use PlaylistTitle field for the actual playlist name (not the video's title)
		videoInfos = infos
		if info.PlaylistTitle != nil {
			playlistTitle = *info.PlaylistTitle
		} else if info.Playlist != nil {
			playlistTitle = *info.Playlist
		} else {
			// Fallback to video title if playlist title not available
			playlistTitle = getStringValue(info.Title)
		}
	} else {
		// infos[0] is just metadata, skip it
		videoInfos = infos[1:]
		playlistTitle = getStringValue(info.Title)
	}

	playlistInfo := &PlaylistInfo{
		ID:     info.ID,
		Title:  playlistTitle,
		URL:    url,
		Videos: make([]*Song, 0),
	}

	// Get playlist count
	if info.PlaylistCount != nil {
		playlistInfo.VideoCount = int(*info.PlaylistCount)
	}

	// Update VideoCount if not set
	if playlistInfo.VideoCount == 0 {
		playlistInfo.VideoCount = len(videoInfos)
	}

	// Get thumbnail from first video if available
	if len(videoInfos) > 0 && len(videoInfos[0].Thumbnails) > 0 {
		playlistInfo.ThumbnailURL = videoInfos[0].Thumbnails[0].URL
	}

	// Determine the domain to use for video URLs (preserve YouTube Music URLs)
	domain := "www.youtube.com"
	if strings.Contains(url, "music.youtube.com") {
		domain = "music.youtube.com"
	}

	// Parse video entries
	for _, entry := range videoInfos {
		videoURL := fmt.Sprintf("https://%s/watch?v=%s", domain, entry.ID)

		// Parse duration
		var duration string
		var isLive bool

		if entry.LiveStatus != nil && (*entry.LiveStatus == ytdlp.ExtractedLiveStatusIsLive ||
			*entry.LiveStatus == ytdlp.ExtractedLiveStatusIsUpcoming) {
			duration = "🔴 LIVE"
			isLive = true
		} else if entry.Duration != nil && *entry.Duration > 0 {
			duration = formatDuration(int(*entry.Duration))
		} else {
			duration = "Unknown"
		}

		// Get thumbnail
		thumbnail := ""
		if len(entry.Thumbnails) > 0 {
			thumbnail = entry.Thumbnails[len(entry.Thumbnails)-1].URL
		}

		song := &Song{
			URL:           videoURL,
			Title:         getStringValue(entry.Title),
			Duration:      duration,
			Thumbnail:     thumbnail,
			Uploader:      getStringValue(entry.Uploader),
			IsLive:        isLive,
			RequestedBy:   requesterName,
			RequestedByID: requesterID,
		}

		playlistInfo.Videos = append(playlistInfo.Videos, song)
	}

	logger.Infof("Retrieved playlist: %s (%d videos)", playlistInfo.Title, len(playlistInfo.Videos))
	return playlistInfo, nil
}

// getStringValue safely extracts a string value from a pointer
func getStringValue(ptr *string) string {
	if ptr != nil {
		return *ptr
	}
	return ""
}

// UpdateYtDlp updates the yt-dlp binary
// This function is deprecated and kept for compatibility
// Use internal/ytdlp package's AutoUpdate() instead
func UpdateYtDlp() error {
	// This is now handled by internal/ytdlp package
	// which provides better update control and weekly checking
	return nil
}

// CheckAvailability checks if a video is available and if it's live
// This is a lightweight check that doesn't fetch full video info
// Uses fast Innertube API (100-300ms) with fallback to yt-dlp (2300ms) on failure
func CheckAvailability(url string) (available bool, isLive bool, err error) {
	// Check cache first to avoid redundant calls
	if cached, ok := availabilityCache.Load(url); ok {
		cachedEntry := cached.(*cachedAvailability)
		if time.Since(cachedEntry.timestamp) < cacheTTL {
			logger.Debugf("[CheckAvailability] Cache hit for: %s (age: %v)", url, time.Since(cachedEntry.timestamp))
			return cachedEntry.result.Available, cachedEntry.result.IsLive, nil
		}
		// Cache expired, remove it
		availabilityCache.Delete(url)
	}

	// Check circuit breaker before making request
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[CheckAvailability] Circuit breaker open: %v", err)
		return false, false, err
	}

	startTime := time.Now()

	client := getInnertubeClient()
	available, isLive, err = client.CheckAvailability(url)

	if err != nil {
		// Innertube failed, fallback to yt-dlp
		logger.Warnf("[CheckAvailability] Innertube failed, falling back to yt-dlp: %v", err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Create ytdlp command with minimal info extraction
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			NoPlaylist().
			JsRuntimes("node").
			SkipDownload()

		// Run yt-dlp to check availability
		result, ytdlpErr := cmd.Run(ctx, url)
		if ytdlpErr != nil {
			ytCircuitBreaker.recordFailure(ytdlpErr)
			checkTime := time.Since(startTime)
			logger.Debugf("[CheckAvailability] yt-dlp also failed after %v: %v", checkTime, ytdlpErr)
			return false, false, ytdlpErr
		}

		// Parse extracted info
		infos, parseErr := result.GetExtractedInfo()
		if parseErr != nil {
			return false, false, parseErr
		}

		if len(infos) == 0 {
			return false, false, fmt.Errorf("video not available")
		}

		info := infos[0]

		// Check if live
		isLive = false
		if info.LiveStatus != nil && (*info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive ||
			*info.LiveStatus == ytdlp.ExtractedLiveStatusIsUpcoming) {
			isLive = true
		}

		available = true
		checkTime := time.Since(startTime)
		logger.Debugf("[CheckAvailability] yt-dlp fallback succeeded in %v", checkTime)
	}

	checkTime := time.Since(startTime)
	logger.Debugf("[CheckAvailability] Check completed in %v for: %s (available: %v, isLive: %v)", checkTime, url, available, isLive)

	// Cache successful result (10-minute TTL to reduce duplicate checks)
	if available {
		availabilityCache.Store(url, &cachedAvailability{
			result: &AvailabilityResult{
				Available: true,
				IsLive:    isLive,
			},
			timestamp: time.Now(),
		})
	}

	// Record success in circuit breaker
	ytCircuitBreaker.recordSuccess()

	return available, isLive, nil
}
