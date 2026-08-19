package youtube

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"

	"github.com/lrstanley/go-ytdlp"
	"github.com/ppalone/ytsearch"
)

func applyJsRuntime(cmd *ytdlp.Command) *ytdlp.Command {
	if rt := ytdlpUpdater.GetJsRuntime(); rt != "" {
		return cmd.JsRuntimes(rt)
	}
	return cmd
}

func SaveStreamFailure(url string, err error) {
	saveVersionResult(url, err)
	ytdlpUpdater.RequestUpdateCheck()
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

var (
	youtubeRegex = regexp.MustCompile(`^(https?://)?(www\.)?(music\.youtube\.com|youtube\.com|youtu\.be)/.+$`)
	searchClient *ytsearch.Client

	availabilityCache = &sync.Map{}
	cacheTTL          = 10 * time.Minute

	ytCircuitBreaker = &circuitBreaker{
		state: circuitClosed,
	}
	circuitOpenThreshold  = 5
	circuitCooldownPeriod = 60 * time.Second
)

func Initialize() error {

	searchClient = ytsearch.NewClient(nil)

	return nil
}

func IsYouTubeURL(query string) bool {
	return youtubeRegex.MatchString(query)
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
				logger.Infof("%s succeeded after %d attempts", operationName, attempt+1)
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

			logger.Warnf("%s failed (attempt %d/%d): %v, retrying in %v",
				operationName, attempt+1, maxRetries, err, delay)
			time.Sleep(delay)
		}
	}

	logger.Errorf("%s failed after %d attempts: %v", operationName, maxRetries, lastErr)
	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func GetVideoInfo(guildID, url, requesterName, requesterID string) (*Song, error) {
	logger.Debugf("Fetching video info for: %s", url)

	client := getInnertubeClient()
	song, innertubeErr := client.GetVideoInfo(guildID, url, requesterName, requesterID)

	if innertubeErr == nil {

		return song, nil
	}

	logger.Warnf("Innertube failed, falling back to yt-dlp: %v", innertubeErr)

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

		logger.Debugf("Voice channel bitrate unknown, using bestaudio")
		return "bestaudio/best"
	}

	bitrateKbps := bitrate / 1000
	logger.Debugf("Voice channel bitrate: %d kbps", bitrateKbps)

	if bitrate <= 32000 {

		logger.Debugf("Using low quality audio (≤32k)")
		return "bestaudio[abr<=32]/bestaudio[abr<=48]/bestaudio[abr<=64]/bestaudio/best"
	} else if bitrate <= 64000 {

		logger.Debugf("Using medium quality audio (≤64k)")
		return "bestaudio[abr<=64]/bestaudio[abr<=96]/bestaudio/best"
	} else if bitrate <= 96000 {

		logger.Debugf("Using high quality audio (≤96k)")
		return "bestaudio[abr<=96]/bestaudio[abr<=128]/bestaudio/best"
	} else if bitrate <= 128000 {

		logger.Debugf("Using very high quality audio (≤128k)")
		return "bestaudio[abr<=128]/bestaudio[abr<=160]/bestaudio/best"
	} else {

		logger.Debugf("Using maximum quality audio")
		return "bestaudio/best"
	}
}

func GetStreamURL(url string, sponsorBlock bool, bitrate int) (string, error) {
	return GetStreamURLContext(context.Background(), url, sponsorBlock, bitrate)
}

func GetStreamURLContext(parent context.Context, url string, sponsorBlock bool, bitrate int) (string, error) {

	if err := ytCircuitBreaker.canAttempt(); err != nil {
		logger.Warnf("Circuit breaker open: %v", err)
		return "", err
	}

	audioFormat := GetOptimalAudioFormat(bitrate)
	logger.Debugf("Getting stream URL for: %s (SponsorBlock: %v, Format: %s)", url, sponsorBlock, audioFormat)

	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
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

		logger.Debugf("Running yt-dlp command for: %s", url)
		result, err := cmd.Run(ctx, url)
		if err != nil {
			logger.Errorf("yt-dlp failed: %v", err)
			ytCircuitBreaker.recordFailure(err)
			saveVersionResult(url, err)
			return fmt.Errorf("failed to get stream URL: %w", err)
		}
		logger.Debugf("yt-dlp completed successfully")

		streamURL = result.Stdout
		if streamURL == "" {
			logger.Errorf("Empty stream URL returned")
			return fmt.Errorf("empty stream URL returned")
		}

		logger.Debugf("Got stream URL (length: %d)", len(streamURL))
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

func getStringValue(ptr *string) string {
	if ptr != nil {
		return *ptr
	}
	return ""
}

func UpdateYtDlp() error {

	return nil
}
