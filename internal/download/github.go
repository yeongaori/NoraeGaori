package download

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"noraegaori/internal/logger"
)

type GitHubRepo struct {
	APIURL string
	WebURL string
	Repo   string
}

var rateLimitWarnings sync.Map

func (r GitHubRepo) FetchLatest(checksumAsset string, extraAssets ...string) (*Release, error) {
	release, err := FetchRelease(r.APIURL + "/repos/" + r.Repo + "/releases/latest")
	if err == nil || !IsRateLimited(err) {
		return release, err
	}

	if _, isWarned := rateLimitWarnings.LoadOrStore(r.Repo, struct{}{}); !isWarned {
		logger.Warnf("GitHub API rate limit reached, reading %s from github.com instead", r.Repo)
	}

	release, webErr := r.fetchLatestFromWeb(checksumAsset, extraAssets)
	if webErr != nil {
		return nil, fmt.Errorf("%w; the github.com fallback failed too: %v", err, webErr)
	}
	return release, nil
}

func (r GitHubRepo) fetchLatestFromWeb(checksumAsset string, extraAssets []string) (*Release, error) {
	tag, err := r.latestTag()
	if err != nil {
		return nil, err
	}

	checksumFile, err := FetchBytes(r.downloadURL(tag, checksumAsset))
	if err != nil {
		return nil, err
	}
	checksums, err := ParseChecksums(bytes.NewReader(checksumFile))
	if err != nil {
		return nil, fmt.Errorf("%s of %s %s: %w", checksumAsset, r.Repo, tag, err)
	}

	names := append(slices.Sorted(maps.Keys(checksums)), checksumAsset)
	names = append(names, extraAssets...)
	assets := make([]Asset, 0, len(names))
	for _, name := range names {
		assets = append(assets, Asset{Name: name, BrowserDownloadURL: r.downloadURL(tag, name)})
	}

	return &Release{TagName: tag, PublishedAt: r.publishedAt(tag), Assets: assets}, nil
}

func (r GitHubRepo) latestTag() (string, error) {
	latestURL := r.WebURL + "/" + r.Repo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request to %s failed: %w", latestURL, err)
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	_, escapedTag, isTagPage := strings.Cut(location, "/releases/tag/")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || !isTagPage || escapedTag == "" || strings.Contains(escapedTag, "/") {
		return "", fmt.Errorf("%s did not redirect to a release tag (status %d, location %q)", latestURL, resp.StatusCode, location)
	}
	return url.PathUnescape(escapedTag)
}

type atomFeed struct {
	Entries []struct {
		ID      string `xml:"id"`
		Updated string `xml:"updated"`
	} `xml:"entry"`
}

func (r GitHubRepo) publishedAt(tag string) string {
	feedURL := r.WebURL + "/" + r.Repo + "/releases.atom"
	body, err := FetchBytes(feedURL)
	if err != nil {
		logger.Debugf("No publish time for %s %s: %v", r.Repo, tag, err)
		return ""
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		logger.Debugf("No publish time for %s %s: %s is not a release feed: %v", r.Repo, tag, feedURL, err)
		return ""
	}
	for _, entry := range feed.Entries {
		if strings.HasSuffix(entry.ID, "/"+tag) {
			return entry.Updated
		}
	}
	logger.Debugf("No publish time for %s %s: the release feed has no entry for it", r.Repo, tag)
	return ""
}

func (r GitHubRepo) downloadURL(tag, name string) string {
	return r.WebURL + "/" + r.Repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}
