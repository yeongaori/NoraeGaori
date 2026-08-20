package youtube

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	ytdlpUpdater "noraegaori/internal/ytdlp"

	"github.com/lrstanley/go-ytdlp"
)

type PlaylistInfo struct {
	ID           string
	Title        string
	URL          string
	ThumbnailURL string
	VideoCount   int
	Videos       []*Song
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
