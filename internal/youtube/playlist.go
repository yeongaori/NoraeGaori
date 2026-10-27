package youtube

import (
	"context"
	"fmt"
	"strings"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/messages"

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
	parsed, ok := parseYouTubeURL(urlStr)
	if !ok {
		return &URLAnalysis{Type: URLTypeVideoOnly}
	}

	if parsed.VideoID != "" && parsed.PlaylistID != "" {
		return &URLAnalysis{
			Type:       URLTypeVideoWithPlaylist,
			VideoID:    parsed.VideoID,
			PlaylistID: parsed.PlaylistID,
		}
	}

	if parsed.PlaylistID != "" {
		return &URLAnalysis{
			Type:       URLTypePurePlaylist,
			PlaylistID: parsed.PlaylistID,
		}
	}

	return &URLAnalysis{
		Type:    URLTypeVideoOnly,
		VideoID: parsed.VideoID,
	}
}

func GetPlaylistInfo(url, requesterName, requesterID string) (*PlaylistInfo, error) {
	logger.Debugf("Fetching playlist info for: %s", url)

	infos, err := fetchPlaylistEntries(url)
	if err != nil {
		return nil, err
	}

	info := infos[0]

	if info.PlaylistCount == nil && len(infos) <= 1 {
		return nil, fmt.Errorf("URL is not a playlist (no entries found)")
	}

	videoInfos, playlistTitle := splitPlaylistEntries(infos)

	playlistInfo := &PlaylistInfo{
		ID:     info.ID,
		Title:  playlistTitle,
		URL:    url,
		Videos: make([]*Song, 0, len(videoInfos)),
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
		playlistInfo.Videos = append(playlistInfo.Videos, playlistSong(domain, entry, requesterName, requesterID))
	}

	logger.Infof("Retrieved playlist: %s (%d videos)", playlistInfo.Title, len(playlistInfo.Videos))
	return playlistInfo, nil
}

func fetchPlaylistEntries(url string) ([]*ytdlp.ExtractedInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := ytdlpCommand().
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
	return infos, nil
}

func splitPlaylistEntries(infos []*ytdlp.ExtractedInfo) ([]*ytdlp.ExtractedInfo, string) {
	info := infos[0]
	if info.Duration != nil {

		if info.PlaylistTitle != nil {
			return infos, *info.PlaylistTitle
		} else if info.Playlist != nil {
			return infos, *info.Playlist
		}

		return infos, getStringValue(info.Title)
	}

	return infos[1:], getStringValue(info.Title)
}

func playlistSong(domain string, entry *ytdlp.ExtractedInfo, requesterName, requesterID string) *Song {
	videoURL := fmt.Sprintf("https://%s/watch?v=%s", domain, entry.ID)

	duration, isLive := durationLabel(entry)
	thumbnail := lastThumbnail(entry)

	return &Song{
		URL:           videoURL,
		Title:         getStringValue(entry.Title),
		Duration:      duration,
		Thumbnail:     thumbnail,
		Uploader:      getStringValue(entry.Uploader),
		IsLive:        isLive,
		RequestedBy:   requesterName,
		RequestedByID: requesterID,
	}
}
