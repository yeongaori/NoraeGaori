package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	userAgent      = "noraegaori-updater"
	requestTimeout = 30 * time.Second
	digestPrefix   = "sha256:"
)

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type Release struct {
	TagName     string  `json:"tag_name"`
	Name        string  `json:"name"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

func (release *Release) FindAsset(name string) (*Asset, bool) {
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i], true
		}
	}
	return nil, false
}

func (release *Release) AssetURL(name string) string {
	if asset, ok := release.FindAsset(name); ok {
		return asset.BrowserDownloadURL
	}
	return ""
}

func (release *Release) AssetDigest(name string) string {
	asset, ok := release.FindAsset(name)
	if !ok || !strings.HasPrefix(asset.Digest, digestPrefix) {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(asset.Digest, digestPrefix))
}

func request(url string) (*http.Response, error) {
	client := &http.Client{Timeout: requestTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &StatusError{URL: url, Code: resp.StatusCode, RateLimited: isRateLimitReply(resp)}
	}
	return resp, nil
}

func isRateLimitReply(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0"
}

type StatusError struct {
	URL         string
	Code        int
	RateLimited bool
}

func (err *StatusError) Error() string {
	if err.RateLimited {
		return fmt.Sprintf("%s returned status %d (GitHub API rate limit reached)", err.URL, err.Code)
	}
	return fmt.Sprintf("%s returned status %d", err.URL, err.Code)
}

func IsRateLimited(err error) bool {
	var statusErr *StatusError
	return errors.As(err, &statusErr) && statusErr.RateLimited
}

func FetchJSON(url string, target any) error {
	resp, err := request(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to parse %s: %w", url, err)
	}
	return nil
}

func FetchBytes(url string) ([]byte, error) {
	resp, err := request(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func FetchRelease(url string) (*Release, error) {
	var release Release
	if err := FetchJSON(url, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

func FetchReleases(url string) ([]*Release, error) {
	var releases []*Release
	if err := FetchJSON(url, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}
