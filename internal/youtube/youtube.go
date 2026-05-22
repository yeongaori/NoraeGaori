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

func applyJsRuntime(cmd *ytdlp.Command) *ytdlp.Command {
	if rt := ytdlpUpdater.GetJsRuntime(); rt != "" {
		return cmd.JsRuntimes(rt)
	}
	return cmd
}

func saveVersionResult(url string, err error) {
	versionmanager := ytdlpUpdater.GetVersionManager()
	if versionmanager == nil {
		return
	}
	version := versionmanager.GetActiveVersion()
	if version == "" {
		return
	}
	if err == nil {
		versionmanager.SaveSuccess(version, url)
	} else {
		versionmanager.SaveError(version, url, err.Error())
	}
}

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

type SearchResult = ytsearch.VideoInfo

type PlaylistInfo struct {
	ID            string
	Title         string
	URL           string
	ThumbnailURL  string
	VideoCount    int
	Videos        []*Song
}

type URLType string

const (
	URLTypePurePlaylist      URLType = "pure_playlist"
	URLTypeVideoWithPlaylist URLType = "video_with_playlist"
	URLTypeVideoOnly         URLType = "video_only"
)

type URLAnalysis struct {
	Type       URLType
	VideoID    string
	PlaylistID string
}

type AvailabilityResult struct {
	Available bool
	Error     string
	IsLive    bool
}

type cachedAvailability struct {
	result    *AvailabilityResult
	timestamp time.Time
}

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
	circuitClosed   circuitState = iota 
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

var (
	
	youtubeRegex = regexp.MustCompile(`^(https?://)?(www\.)?(music\.youtube\.com|youtube\.com|youtu\.be)/.+$`)
	searchClient *ytsearch.Client

	
	availabilityCache = &sync.Map{}
	cacheTTL          = 10 * time.Minute 

	
	ytCircuitBreaker = &circuitBreaker{
		state: circuitClosed,
	}
	circuitOpenThreshold   = 5               
	circuitCooldownPeriod  = 60 * time.Second 
)

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
		logger.Info("[CircuitBreaker] Test request succeeded, closing circuit")
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
		logger.Warnf("[CircuitBreaker] Opening circuit after %d consecutive rate limit errors", cb.consecutiveFails)
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
			logger.Info("[CircuitBreaker] Cooldown complete, entering half-open state (testing)")
			return nil
		}
		return fmt.Errorf("YouTube rate limit exceeded, please wait %v before trying again",
			circuitCooldownPeriod-time.Since(lastFailure))
	case circuitHalfOpen:
		return nil 
	}
	return nil
}

func Initialize() error {
	
	searchClient = ytsearch.NewClient(nil)

	return nil
}

func IsYouTubeURL(query string) bool {
	return youtubeRegex.MatchString(query)
}

func Search(guildID, query string, requesterName, requesterID string) (*Song, error) {
	if IsYouTubeURL(query) {
		
		return GetVideoInfo(guildID, query, requesterName, requesterID)
	}

	
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

	
	return GetVideoInfo(guildID, videoURL, requesterName, requesterID)
}

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

	
	if len(response.Results) > limit {
		return response.Results[:limit], nil
	}

	return response.Results, nil
}

func CheckVideoAvailability(guildID, url string) (*AvailabilityResult, error) {
	cacheKey := guildID + "|" + url
	
	if cached, ok := availabilityCache.Load(cacheKey); ok {
		cachedEntry := cached.(*cachedAvailability)
		if time.Since(cachedEntry.timestamp) < cacheTTL {
			logger.Debugf("[Availability] Cache hit for: %s (age: %v)", url, time.Since(cachedEntry.timestamp))
			return cachedEntry.result, nil
		}
		
		availabilityCache.Delete(cacheKey)
		logger.Debugf("[Availability] Cache expired for: %s", url)
	}

	
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[Availability] Circuit breaker open: %v", err)
		return nil, err
	}

	startTime := time.Now()
	logger.Debugf("[Availability] Starting check for: %s", url)

	client := getInnertubeClient()
	availResult, innertubeErr := client.CheckVideoAvailability(guildID, url)

	if innertubeErr != nil {
		
		logger.Warnf("[Availability] Innertube failed, falling back to yt-dlp: %v", innertubeErr)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		
		cmd := applyJsRuntime(ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			FlatPlaylist()).
			SkipDownload()

		
		result, err := cmd.Run(ctx, url)
		if err != nil {
			
			ytCircuitBreaker.recordFailure(err)
			saveVersionResult(url, err)
			checkTime := time.Since(startTime)
			logger.Debugf("[Availability] yt-dlp error after %v: %v", checkTime, err)

			
			errorMsg := strings.ToLower(err.Error())
			if strings.Contains(errorMsg, "video unavailable") ||
				strings.Contains(errorMsg, "private video") ||
				strings.Contains(errorMsg, "deleted video") ||
				strings.Contains(errorMsg, "age-restricted") ||
				strings.Contains(errorMsg, "not available in your country") ||
				strings.Contains(errorMsg, "geo") {
				logger.Debugf("[Availability] Video blocked by error: %v (%v)", err, checkTime)
				unavailResult := &AvailabilityResult{
					Available: false,
					Error:     err.Error(),
				}
				
				availabilityCache.Store(cacheKey, &cachedAvailability{
					result:    unavailResult,
					timestamp: time.Now(),
				})
				return unavailResult, nil
			}

			
			return nil, fmt.Errorf("failed to check availability: %w", err)
		}

		
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

		
		if info.AgeLimit != nil && *info.AgeLimit > 0 {
			unavailableReasons = append(unavailableReasons, messages.T(guildID).YouTube.ErrorAgeVerification)
			logger.Debugf("[Availability] age_limit: %g", *info.AgeLimit)
		}

		
		isLive := info.IsLive != nil && *info.IsLive ||
			(info.LiveStatus != nil && (*info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive ||
				*info.LiveStatus == ytdlp.ExtractedLiveStatusIsUpcoming))
		if isLive {
			logger.Debugf("[Availability] \"%s\" is a LIVE stream", getStringValue(info.Title))
		}

		
		if info.Availability != nil {
			availability := strings.ToLower(string(*info.Availability))
			logger.Debugf("[Availability] availability: %s", availability)
			
			if availability != "public" && availability != "unlisted" {
				unavailableReasons = append(unavailableReasons, messages.T(guildID).YouTube.ErrorRegionRestricted)
			}
		}

		
		if info.Title != nil {
			title := strings.ToLower(*info.Title)
			if strings.Contains(title, "[private video]") ||
				strings.Contains(title, "[deleted video]") ||
				strings.Contains(title, "private video") ||
				strings.Contains(title, "deleted video") {
				unavailableReasons = append(unavailableReasons, messages.T(guildID).YouTube.ErrorPrivateOrDeleted)
				logger.Debugf("[Availability] title_indicates_unavailable: true")
			}
		}

		if len(unavailableReasons) > 0 {
			errorMsg := strings.Join(unavailableReasons, ", ")
			logger.Debugf("[Availability] \"%s\" unavailable: %s (%v)", getStringValue(info.Title), errorMsg, checkTime)
			availResult = &AvailabilityResult{
				Available: false,
				Error:     errorMsg,
				IsLive:    isLive,
			}
		} else {
			logger.Debugf("[Availability] \"%s\" is available (%v)", getStringValue(info.Title), checkTime)
			availResult = &AvailabilityResult{
				Available: true,
				IsLive:    isLive,
			}
		}
	}

	
	availabilityCache.Store(cacheKey, &cachedAvailability{
		result:    availResult,
		timestamp: time.Now(),
	})
	logger.Debugf("[Availability] Cached result for: %s", url)

	
	ytCircuitBreaker.recordSuccess()
	saveVersionResult(url, nil)

	return availResult, nil
}

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

		
		errorMsg := strings.ToLower(err.Error())
		isRetryable := strings.Contains(errorMsg, "network") ||
			strings.Contains(errorMsg, "timeout") ||
			strings.Contains(errorMsg, "rate limit") ||
			strings.Contains(errorMsg, "too many requests") ||
			strings.Contains(errorMsg, "connection") ||
			strings.Contains(errorMsg, "temporary failure")

		if !isRetryable {
			
			return err
		}

		if attempt < maxRetries-1 {
			
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

func GetVideoInfo(guildID, url, requesterName, requesterID string) (*Song, error) {
	logger.Debugf("Fetching video info for: %s", url)

	client := getInnertubeClient()
	song, innertubeErr := client.GetVideoInfo(guildID, url, requesterName, requesterID)

	if innertubeErr == nil {
		
		return song, nil
	}

	
	logger.Warnf("[GetVideoInfo] Innertube failed, falling back to yt-dlp: %v", innertubeErr)

	
	availability, err := CheckVideoAvailability(guildID, url)
	if err != nil {
		logger.Warnf("Failed to check video availability (continuing anyway): %v", err)
	} else if !availability.Available {
		
		errMsg := fmt.Errorf("video is not available: %s", availability.Error)
		return nil, parseYtDlpError(guildID, errMsg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result *ytdlp.Result

	
	retryErr := retryWithBackoff(func() error {
		
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			NoPlaylist()
		cmd = applyJsRuntime(cmd).
			Format("bestaudio/best")

		
		var err error
		result, err = cmd.Run(ctx, url)
		if err != nil {
			return fmt.Errorf("failed to get video info: %w", err)
		}
		return nil
	}, "GetVideoInfo")

	if retryErr != nil {
		
		return nil, parseYtDlpError(guildID, retryErr)
	}

	
	infos, err := result.GetExtractedInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to parse video info: %w", err)
	}

	if len(infos) == 0 {
		return nil, fmt.Errorf("no video info returned")
	}

	info := infos[0]

	
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

	
	thumbnail := ""
	if len(info.Thumbnails) > 0 {
		
		thumbnail = info.Thumbnails[len(info.Thumbnails)-1].URL
	}

	
	uploader := "Unknown"
	if info.Uploader != nil && *info.Uploader != "" {
		uploader = *info.Uploader
	} else if info.Channel != nil && *info.Channel != "" {
		uploader = *info.Channel
	}

	
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

func GetOptimalAudioFormat(bitrate int) string {
	if bitrate <= 0 {
		
		logger.Debugf("[AudioFormat] Voice channel bitrate unknown, using bestaudio")
		return "bestaudio/best"
	}

	
	
	
	
	
	
	

	bitrateKbps := bitrate / 1000
	logger.Debugf("[AudioFormat] Voice channel bitrate: %d kbps", bitrateKbps)

	
	
	
	if bitrate <= 32000 {
		
		logger.Debugf("[AudioFormat] Using low quality audio (≤32k)")
		return "bestaudio[abr<=32]/bestaudio[abr<=48]/bestaudio[abr<=64]/bestaudio/best"
	} else if bitrate <= 64000 {
		
		logger.Debugf("[AudioFormat] Using medium quality audio (≤64k)")
		return "bestaudio[abr<=64]/bestaudio[abr<=96]/bestaudio/best"
	} else if bitrate <= 96000 {
		
		logger.Debugf("[AudioFormat] Using high quality audio (≤96k)")
		return "bestaudio[abr<=96]/bestaudio[abr<=128]/bestaudio/best"
	} else if bitrate <= 128000 {
		
		logger.Debugf("[AudioFormat] Using very high quality audio (≤128k)")
		return "bestaudio[abr<=128]/bestaudio[abr<=160]/bestaudio/best"
	} else {
		
		logger.Debugf("[AudioFormat] Using maximum quality audio")
		return "bestaudio/best"
	}
}

func GetStreamURL(url string, sponsorBlock bool, bitrate int) (string, error) {
	
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[GetStreamURL] Circuit breaker open: %v", err)
		return "", err
	}

	audioFormat := GetOptimalAudioFormat(bitrate)
	logger.Debugf("Getting stream URL for: %s (SponsorBlock: %v, Format: %s)", url, sponsorBlock, audioFormat)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var streamURL string

	
	retryErr := retryWithBackoff(func() error {
		
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			GetURL().
			NoPlaylist()
		cmd = applyJsRuntime(cmd).
			Format(audioFormat)

		
		if sponsorBlock {
			cmd = cmd.SponsorblockMark("all").
				SponsorblockRemove("sponsor,selfpromo,interaction,intro,outro")
		}

		
		logger.Debugf("[GetStreamURL] Running yt-dlp command for: %s", url)
		result, err := cmd.Run(ctx, url)
		if err != nil {
			logger.Errorf("[GetStreamURL] yt-dlp failed: %v", err)
			ytCircuitBreaker.recordFailure(err) 
			saveVersionResult(url, err)
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

	
	ytCircuitBreaker.recordSuccess()
	saveVersionResult(url, nil)

	return streamURL, nil
}

func formatDuration(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

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

func IsLiveStreamActive(url string) (bool, error) {
	logger.Debugf("Checking if live stream is active: %s", url)

	client := getInnertubeClient()
	available, isLive, err := client.CheckAvailability(url)

	if err == nil {
		
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

	
	logger.Warnf("[IsLiveStreamActive] Innertube failed, falling back to yt-dlp: %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	
	cmd := ytdlp.New().
		SetExecutable(ytdlpUpdater.GetBinaryPath()).
		DumpJSON().
		NoPlaylist()
	cmd = applyJsRuntime(cmd).
		Format("bestaudio/best")

	
	result, ytdlpErr := cmd.Run(ctx, url)
	if ytdlpErr != nil {
		return false, fmt.Errorf("failed to get video info: %w", ytdlpErr)
	}

	
	infos, parseErr := result.GetExtractedInfo()
	if parseErr != nil {
		return false, fmt.Errorf("failed to parse video info: %w", parseErr)
	}

	if len(infos) == 0 {
		return false, fmt.Errorf("no video info returned")
	}

	info := infos[0]

	
	if info.LiveStatus != nil && *info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive {
		logger.Debugf("Live stream is active: %s", url)
		return true, nil
	}

	logger.Debugf("Live stream is not active: %s", url)
	return false, nil
}

func CheckIfLive(url string) (bool, error) {
	return IsLiveStreamActive(url)
}

func CheckIfLiveStreamEnded(url string) (bool, error) {
	logger.Debugf("Checking if live stream has ended: %s", url)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var info *ytdlp.ExtractedInfo
	var infos []*ytdlp.ExtractedInfo

	
	retryErr := retryWithBackoff(func() error {
		
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			FlatPlaylist()
		cmd = applyJsRuntime(cmd).
			NoPlaylist()

		
		result, err := cmd.Run(ctx, url)
		if err != nil {
			return fmt.Errorf("failed to get video info: %w", err)
		}

		
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

	
	
	isStillLive := info.IsLive != nil && *info.IsLive ||
		(info.LiveStatus != nil && *info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive)

	if !isStillLive {
		logger.Infof("Live stream has ended: %s", url)
		return true, nil 
	}

	logger.Debugf("Live stream is still active: %s", url)
	return false, nil 
}

func AnalyzeYouTubeURL(urlStr string) *URLAnalysis {
	
	if !regexp.MustCompile(`^https?://`).MatchString(urlStr) {
		urlStr = "https://" + urlStr
	}

	
	playlistRegex := regexp.MustCompile(`youtube\.com/playlist\?list=([a-zA-Z0-9_-]+)`)
	if matches := playlistRegex.FindStringSubmatch(urlStr); len(matches) > 1 {
		return &URLAnalysis{
			Type:       URLTypePurePlaylist,
			PlaylistID: matches[1],
		}
	}

	
	videoWithListRegex := regexp.MustCompile(`[?&]v=([a-zA-Z0-9_-]+).*[?&]list=([a-zA-Z0-9_-]+)`)
	if matches := videoWithListRegex.FindStringSubmatch(urlStr); len(matches) > 2 {
		return &URLAnalysis{
			Type:       URLTypeVideoWithPlaylist,
			VideoID:    matches[1],
			PlaylistID: matches[2],
		}
	}

	
	youtuBeRegex := regexp.MustCompile(`youtu\.be/([a-zA-Z0-9_-]+).*[?&]list=([a-zA-Z0-9_-]+)`)
	if matches := youtuBeRegex.FindStringSubmatch(urlStr); len(matches) > 2 {
		return &URLAnalysis{
			Type:       URLTypeVideoWithPlaylist,
			VideoID:    matches[1],
			PlaylistID: matches[2],
		}
	}

	
	watchRegex := regexp.MustCompile(`[?&]v=([a-zA-Z0-9_-]+)`)
	if matches := watchRegex.FindStringSubmatch(urlStr); len(matches) > 1 {
		return &URLAnalysis{
			Type:    URLTypeVideoOnly,
			VideoID: matches[1],
		}
	}

	
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

func GetPlaylistInfo(url, requesterName, requesterID string) (*PlaylistInfo, error) {
	logger.Debugf("Fetching playlist info for: %s", url)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	
	
	
	
	
	cmd := ytdlp.New().
		SetExecutable(ytdlpUpdater.GetBinaryPath()).
		ExtractorArgs("youtube:lang=" + messages.Lang()).
		DumpJSON().
		FlatPlaylist()
	cmd = applyJsRuntime(cmd).
		IgnoreErrors()

	
	result, err := cmd.Run(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to get playlist info: %w", err)
	}

	
	infos, err := result.GetExtractedInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to parse playlist info: %w", err)
	}

	if len(infos) == 0 {
		return nil, fmt.Errorf("no playlist info returned")
	}

	info := infos[0]

	
	
	if info.PlaylistCount == nil && len(infos) <= 1 {
		return nil, fmt.Errorf("URL is not a playlist (no entries found)")
	}

	
	
	
	
	var videoInfos []*ytdlp.ExtractedInfo
	var playlistTitle string

	if info.Duration != nil {
		
		
		videoInfos = infos
		if info.PlaylistTitle != nil {
			playlistTitle = *info.PlaylistTitle
		} else if info.Playlist != nil {
			playlistTitle = *info.Playlist
		} else {
			
			playlistTitle = getStringValue(info.Title)
		}
	} else {
		
		videoInfos = infos[1:]
		playlistTitle = getStringValue(info.Title)
	}

	playlistInfo := &PlaylistInfo{
		ID:     info.ID,
		Title:  playlistTitle,
		URL:    url,
		Videos: make([]*Song, 0),
	}

	
	if info.PlaylistCount != nil {
		playlistInfo.VideoCount = int(*info.PlaylistCount)
	}

	
	if playlistInfo.VideoCount == 0 {
		playlistInfo.VideoCount = len(videoInfos)
	}

	
	if len(videoInfos) > 0 && len(videoInfos[0].Thumbnails) > 0 {
		playlistInfo.ThumbnailURL = videoInfos[0].Thumbnails[0].URL
	}

	
	domain := "www.youtube.com"
	if strings.Contains(url, "music.youtube.com") {
		domain = "music.youtube.com"
	}

	
	for _, entry := range videoInfos {
		videoURL := fmt.Sprintf("https://%s/watch?v=%s", domain, entry.ID)

		
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

func getStringValue(ptr *string) string {
	if ptr != nil {
		return *ptr
	}
	return ""
}

func UpdateYtDlp() error {
	
	
	return nil
}

func CheckAvailability(url string) (available bool, isLive bool, err error) {
	
	if cached, ok := availabilityCache.Load(url); ok {
		cachedEntry := cached.(*cachedAvailability)
		if time.Since(cachedEntry.timestamp) < cacheTTL {
			logger.Debugf("[CheckAvailability] Cache hit for: %s (age: %v)", url, time.Since(cachedEntry.timestamp))
			return cachedEntry.result.Available, cachedEntry.result.IsLive, nil
		}
		
		availabilityCache.Delete(url)
	}

	
	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("[CheckAvailability] Circuit breaker open: %v", err)
		return false, false, err
	}

	startTime := time.Now()

	client := getInnertubeClient()
	available, isLive, err = client.CheckAvailability(url)

	if err != nil {
		
		logger.Warnf("[CheckAvailability] Innertube failed, falling back to yt-dlp: %v", err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		
		cmd := ytdlp.New().
			SetExecutable(ytdlpUpdater.GetBinaryPath()).
			DumpJSON().
			NoPlaylist()
		cmd = applyJsRuntime(cmd).
			SkipDownload()

		
		result, ytdlpErr := cmd.Run(ctx, url)
		if ytdlpErr != nil {
			ytCircuitBreaker.recordFailure(ytdlpErr)
			saveVersionResult(url, ytdlpErr)
			checkTime := time.Since(startTime)
			logger.Debugf("[CheckAvailability] yt-dlp also failed after %v: %v", checkTime, ytdlpErr)
			return false, false, ytdlpErr
		}

		
		infos, parseErr := result.GetExtractedInfo()
		if parseErr != nil {
			return false, false, parseErr
		}

		if len(infos) == 0 {
			return false, false, fmt.Errorf("video not available")
		}

		info := infos[0]

		
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

	
	if available {
		availabilityCache.Store(url, &cachedAvailability{
			result: &AvailabilityResult{
				Available: true,
				IsLive:    isLive,
			},
			timestamp: time.Now(),
		})
	}

	
	ytCircuitBreaker.recordSuccess()
	saveVersionResult(url, nil)

	return available, isLive, nil
}
