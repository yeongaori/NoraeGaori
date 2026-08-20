package youtube

import (
	"context"
	"fmt"
	"strings"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	ytdlpUpdater "noraegaori/internal/ytdlp"

	"github.com/lrstanley/go-ytdlp"
)

type AvailabilityResult struct {
	Available bool
	Error     string
	IsLive    bool
}

type cachedAvailability struct {
	result    *AvailabilityResult
	timestamp time.Time
}

var checkAvailabilityViaInnertube = func(guildID, url string) (*AvailabilityResult, error) {
	return getInnertubeClient().CheckVideoAvailability(guildID, url)
}

var runYtDlpAvailability = func(ctx context.Context, url string) (*ytdlp.Result, error) {
	cmd := applyJsRuntime(ytdlp.New().
		SetExecutable(ytdlpUpdater.GetBinaryPath()).
		DumpJSON().
		FlatPlaylist()).
		SkipDownload()

	return cmd.Run(ctx, url)
}

func checkAvailabilityWithYtDlp(guildID, url, cacheKey string, startTime time.Time) (*AvailabilityResult, bool, error) {
	var availResult *AvailabilityResult

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runYtDlpAvailability(ctx, url)
	if err != nil {

		ytCircuitBreaker.recordFailure(err)
		saveVersionResult(url, err)
		checkTime := time.Since(startTime)
		logger.Debugf("yt-dlp error after %v: %v", checkTime, err)

		errorMsg := strings.ToLower(err.Error())
		if strings.Contains(errorMsg, "video unavailable") ||
			strings.Contains(errorMsg, "private video") ||
			strings.Contains(errorMsg, "deleted video") ||
			strings.Contains(errorMsg, "age-restricted") ||
			strings.Contains(errorMsg, "not available in your country") ||
			strings.Contains(errorMsg, "geo") {
			logger.Debugf("Video blocked by error: %v (%v)", err, checkTime)
			unavailResult := &AvailabilityResult{
				Available: false,
				Error:     err.Error(),
			}

			availabilityCache.Store(cacheKey, &cachedAvailability{
				result:    unavailResult,
				timestamp: time.Now(),
			})
			return unavailResult, true, nil
		}

		return nil, true, fmt.Errorf("failed to check availability: %w", err)
	}

	infos, err := result.GetExtractedInfo()
	if err != nil {
		return nil, true, fmt.Errorf("failed to parse video info: %w", err)
	}

	if len(infos) == 0 {
		return &AvailabilityResult{
			Available: false,
			Error:     "no video info returned",
		}, true, nil
	}

	info := infos[0]
	checkTime := time.Since(startTime)
	logger.Debugf("yt-dlp info fetched in %v for: %s", checkTime, getStringValue(info.Title))

	unavailableReasons := []string{}

	if info.AgeLimit != nil && *info.AgeLimit > 0 {
		unavailableReasons = append(unavailableReasons, messages.T(guildID).YouTube.ErrorAgeVerification)
		logger.Debugf("age_limit: %g", *info.AgeLimit)
	}

	isLive := info.IsLive != nil && *info.IsLive ||
		(info.LiveStatus != nil && (*info.LiveStatus == ytdlp.ExtractedLiveStatusIsLive ||
			*info.LiveStatus == ytdlp.ExtractedLiveStatusIsUpcoming))
	if isLive {
		logger.Debugf("\"%s\" is a LIVE stream", getStringValue(info.Title))
	}

	if info.Availability != nil {
		availability := strings.ToLower(string(*info.Availability))
		logger.Debugf("availability: %s", availability)

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
			logger.Debugf("title_indicates_unavailable: true")
		}
	}

	if len(unavailableReasons) > 0 {
		errorMsg := strings.Join(unavailableReasons, ", ")
		logger.Debugf("\"%s\" unavailable: %s (%v)", getStringValue(info.Title), errorMsg, checkTime)
		availResult = &AvailabilityResult{
			Available: false,
			Error:     errorMsg,
			IsLive:    isLive,
		}
	} else {
		logger.Debugf("\"%s\" is available (%v)", getStringValue(info.Title), checkTime)
		availResult = &AvailabilityResult{
			Available: true,
			IsLive:    isLive,
		}
	}

	return availResult, false, nil
}

func CheckVideoAvailability(guildID, url string) (*AvailabilityResult, error) {
	cacheKey := guildID + "|" + url

	if cached, ok := availabilityCache.Load(cacheKey); ok {
		cachedEntry := cached.(*cachedAvailability)
		if time.Since(cachedEntry.timestamp) < cacheTTL {
			logger.Debugf("Cache hit for: %s (age: %v)", url, time.Since(cachedEntry.timestamp))
			return cachedEntry.result, nil
		}

		availabilityCache.Delete(cacheKey)
		logger.Debugf("Cache expired for: %s", url)
	}

	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("Circuit breaker open: %v", err)
		return nil, err
	}

	startTime := time.Now()
	logger.Debugf("Starting check for: %s", url)

	availResult, innertubeErr := checkAvailabilityViaInnertube(guildID, url)

	if innertubeErr != nil {

		logger.Warnf("Innertube failed, falling back to yt-dlp: %v", innertubeErr)

		fallbackResult, done, fallbackErr := checkAvailabilityWithYtDlp(guildID, url, cacheKey, startTime)
		if done {
			return fallbackResult, fallbackErr
		}
		availResult = fallbackResult
	}

	availabilityCache.Store(cacheKey, &cachedAvailability{
		result:    availResult,
		timestamp: time.Now(),
	})
	logger.Debugf("Cached result for: %s", url)

	ytCircuitBreaker.recordSuccess()
	saveVersionResult(url, nil)

	return availResult, nil
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

	logger.Warnf("Innertube failed, falling back to yt-dlp: %v", err)

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

func CheckAvailability(url string) (available bool, isLive bool, err error) {

	if cached, ok := availabilityCache.Load(url); ok {
		cachedEntry := cached.(*cachedAvailability)
		if time.Since(cachedEntry.timestamp) < cacheTTL {
			logger.Debugf("Cache hit for: %s (age: %v)", url, time.Since(cachedEntry.timestamp))
			return cachedEntry.result.Available, cachedEntry.result.IsLive, nil
		}

		availabilityCache.Delete(url)
	}

	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("Circuit breaker open: %v", err)
		return false, false, err
	}

	startTime := time.Now()

	client := getInnertubeClient()
	available, isLive, err = client.CheckAvailability(url)

	if err != nil {

		logger.Warnf("Innertube failed, falling back to yt-dlp: %v", err)

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
			logger.Debugf("yt-dlp also failed after %v: %v", checkTime, ytdlpErr)
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
		logger.Debugf("yt-dlp fallback succeeded in %v", checkTime)
	}

	checkTime := time.Since(startTime)
	logger.Debugf("Check completed in %v for: %s (available: %v, isLive: %v)", checkTime, url, available, isLive)

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
