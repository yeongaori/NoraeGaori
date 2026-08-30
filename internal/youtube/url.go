package youtube

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var ErrUnsupportedYouTubeURL = errors.New("unsupported youtube url")

var (
	schemePrefixRegex = regexp.MustCompile(`^https?://`)
	videoIDRegex      = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)
	playlistIDRegex   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	allowedHosts = map[string]bool{
		"youtube.com":       true,
		"www.youtube.com":   true,
		"m.youtube.com":     true,
		"music.youtube.com": true,
		"youtu.be":          true,
		"www.youtu.be":      true,
	}

	videoPathPrefixes = map[string]bool{
		"shorts": true,
		"live":   true,
		"embed":  true,
		"v":      true,
	}
)

type parsedYouTubeURL struct {
	Host       string
	Segments   []string
	VideoID    string
	PlaylistID string
}

func parseYouTubeURL(raw string) (*parsedYouTubeURL, bool) {
	trimmed := strings.TrimSpace(raw)
	if !schemePrefixRegex.MatchString(trimmed) {
		trimmed = "https://" + trimmed
	}

	address, err := url.Parse(trimmed)
	if err != nil || address.User != nil || address.Port() != "" {
		return nil, false
	}

	host := strings.ToLower(address.Hostname())
	if !allowedHosts[host] {
		return nil, false
	}

	segments := pathSegments(address.Path)
	query := address.Query()

	parsed := &parsedYouTubeURL{
		Host:     host,
		Segments: segments,
		VideoID:  findVideoID(host, segments, query.Get("v")),
	}

	if list := query.Get("list"); playlistIDRegex.MatchString(list) {
		parsed.PlaylistID = list
	}

	return parsed, true
}

func pathSegments(path string) []string {
	segments := make([]string, 0, 2)
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func findVideoID(host string, segments []string, queryID string) string {
	if videoIDRegex.MatchString(queryID) {
		return queryID
	}

	if isShortLinkHost(host) && len(segments) > 0 && videoIDRegex.MatchString(segments[0]) {
		return segments[0]
	}

	if len(segments) > 1 && videoPathPrefixes[strings.ToLower(segments[0])] && videoIDRegex.MatchString(segments[1]) {
		return segments[1]
	}

	return ""
}

func isShortLinkHost(host string) bool {
	return host == "youtu.be" || host == "www.youtu.be"
}

func IsYouTubeURL(query string) bool {
	_, ok := parseYouTubeURL(query)
	return ok
}
