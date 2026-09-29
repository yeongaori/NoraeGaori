package download_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/download"
	"noraegaori/tests/testutil/logtest"
	"noraegaori/tests/testutil/releasetest"
)

const (
	linuxBuild   = "ffmpeg-master-latest-linux64-gpl.tar.xz"
	windowsBuild = "ffmpeg-master-latest-win64-gpl.zip"
)

type fakeGitHub struct {
	api *releasetest.Server
	web *releasetest.Server
}

func serveGitHub(t *testing.T) fakeGitHub {
	t.Helper()

	return fakeGitHub{api: releasetest.Serve(t), web: releasetest.Serve(t)}
}

func (gh fakeGitHub) repo(name string) download.GitHubRepo {
	return download.GitHubRepo{APIURL: gh.api.URL(""), WebURL: gh.web.URL(""), Repo: name}
}

func (gh fakeGitHub) publishWebRelease(repo string) {
	gh.web.RedirectLatest(repo, "latest")
	gh.web.Files["/"+repo+"/releases/download/latest/checksums.sha256"] = releasetest.SumA + "  " + linuxBuild + "\n" + releasetest.SumB + "  " + windowsBuild + "\n"
	gh.web.PublishFeed(repo,
		releasetest.FeedEntry{Tag: "autobuild-2026-09-24", Updated: "2026-09-24T01:02:03Z"},
		releasetest.FeedEntry{Tag: "latest", Updated: "2026-09-25T18:45:57Z"},
	)
}

func (gh fakeGitHub) rateLimit(repo string) {
	gh.api.RateLimit("/repos/" + repo + "/releases/latest")
	gh.publishWebRelease(repo)
}

func TestLatestReleaseUsesTheAPIWhenItAnswers(t *testing.T) {
	gh := serveGitHub(t)
	gh.api.PublishRelease(t, "/repos/owner/answers/releases/latest", download.Release{TagName: "from-api"})
	gh.publishWebRelease("owner/answers")

	release, err := gh.repo("owner/answers").FetchLatest("checksums.sha256")
	if err != nil || release.TagName != "from-api" {
		t.Fatalf("got %+v, %v, want the API's release, not github.com's", release, err)
	}
}

func TestLatestReleaseFallsBackWhenTheAPIIsRateLimited(t *testing.T) {
	read := logtest.Capture(t)
	gh := serveGitHub(t)
	name := fmt.Sprintf("owner/limited-%d", time.Now().UnixNano())
	gh.rateLimit(name)
	repo := gh.repo(name)

	release, err := repo.FetchLatest("checksums.sha256", "checksums.sha256.sig")
	if err != nil {
		t.Fatalf("FetchLatest returned %v, want the github.com release", err)
	}
	if _, err := repo.FetchLatest("checksums.sha256"); err != nil {
		t.Fatalf("second FetchLatest returned %v, want nil", err)
	}

	if release.TagName != "latest" {
		t.Errorf("got tag %q, want the tag from the releases/latest redirect", release.TagName)
	}
	if release.PublishedAt != "2026-09-25T18:45:57Z" {
		t.Errorf("got published %q, want the tag's entry in the release feed", release.PublishedAt)
	}
	base := gh.web.URL("/" + name + "/releases/download/latest/")
	for _, asset := range []string{linuxBuild, windowsBuild, "checksums.sha256", "checksums.sha256.sig"} {
		if got := release.AssetURL(asset); got != base+asset {
			t.Errorf("got %q for %s, want %q", got, asset, base+asset)
		}
	}
	if len(release.Assets) != 4 {
		t.Errorf("got %d assets, want the two listed builds, the checksum file and its signature", len(release.Assets))
	}
	if count := strings.Count(read(), "GitHub API rate limit reached, reading "+name+" from github.com instead"); count != 1 {
		t.Errorf("warned %d times, want once per repository", count)
	}
}

func TestA429FallsBackToGitHubWeb(t *testing.T) {
	gh := serveGitHub(t)
	gh.api.Status["/repos/owner/toomany/releases/latest"] = http.StatusTooManyRequests
	gh.publishWebRelease("owner/toomany")

	release, err := gh.repo("owner/toomany").FetchLatest("checksums.sha256")
	if err != nil || release.TagName != "latest" {
		t.Fatalf("got %+v, %v, want the github.com release", release, err)
	}
}

func TestAForbiddenReplyThatIsNotARateLimitDoesNotFallBack(t *testing.T) {
	gh := serveGitHub(t)
	gh.api.Status["/repos/owner/forbidden/releases/latest"] = http.StatusForbidden
	gh.publishWebRelease("owner/forbidden")

	_, err := gh.repo("owner/forbidden").FetchLatest("checksums.sha256")
	var statusErr *download.StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusForbidden || download.IsRateLimited(err) {
		t.Fatalf("got %v, want the plain 403 without trying github.com", err)
	}
}

func TestFallbackErrorNamesTheRateLimit(t *testing.T) {
	gh := serveGitHub(t)
	gh.api.RateLimit("/repos/owner/nowhere/releases/latest")
	gh.web.Files["/owner/nowhere/releases/latest"] = "<html>no redirect</html>"

	_, err := gh.repo("owner/nowhere").FetchLatest("checksums.sha256")
	if err == nil {
		t.Fatal("got nil, want an error when github.com has no tag redirect")
	}
	if !download.IsRateLimited(err) || !strings.Contains(err.Error(), "GitHub API rate limit reached") || !strings.Contains(err.Error(), "did not redirect to a release tag") {
		t.Errorf("got %q, want the rate limit and the fallback failure named", err)
	}
}

func TestFallbackKeepsTheReleaseWithoutAFeed(t *testing.T) {
	gh := serveGitHub(t)
	gh.rateLimit("owner/nofeed")
	delete(gh.web.Files, "/owner/nofeed/releases.atom")

	release, err := gh.repo("owner/nofeed").FetchLatest("checksums.sha256")
	if err != nil {
		t.Fatalf("FetchLatest returned %v, want nil", err)
	}
	if release.PublishedAt != "" || release.AssetURL(windowsBuild) == "" {
		t.Errorf("got %+v, want the assets without a publish time", release)
	}
}

func TestFallbackLeavesThePublishTimeEmptyWithoutTheTagsFeedEntry(t *testing.T) {
	gh := serveGitHub(t)
	gh.rateLimit("owner/otherfeed")
	gh.web.PublishFeed("owner/otherfeed", releasetest.FeedEntry{Tag: "autobuild-2026-09-24", Updated: "2026-09-24T01:02:03Z"})

	release, err := gh.repo("owner/otherfeed").FetchLatest("checksums.sha256")
	if err != nil {
		t.Fatalf("FetchLatest returned %v, want nil", err)
	}
	if release.PublishedAt != "" {
		t.Errorf("got published %q, want no time rather than another release's", release.PublishedAt)
	}

	gh.web.Files["/owner/otherfeed/releases.atom"] = "not xml"
	if release, err := gh.repo("owner/otherfeed").FetchLatest("checksums.sha256"); err != nil || release.PublishedAt != "" {
		t.Errorf("got %+v, %v, want the release without a time for an unreadable feed", release, err)
	}
}

func TestFallbackRejectsAChecksumFileItCannotRead(t *testing.T) {
	gh := serveGitHub(t)
	gh.rateLimit("owner/badsums")
	gh.web.Files["/owner/badsums/releases/download/latest/checksums.sha256"] = "not a checksum file\n"

	if _, err := gh.repo("owner/badsums").FetchLatest("checksums.sha256"); err == nil || !download.IsRateLimited(err) {
		t.Errorf("got %v, want the unreadable checksum file reported with the rate limit", err)
	}
}

func TestRateLimitedStatusNamesTheLimit(t *testing.T) {
	limited := &download.StatusError{URL: "https://api.example/x", Code: http.StatusForbidden, RateLimited: true}
	plain := &download.StatusError{URL: "https://api.example/x", Code: http.StatusForbidden}

	if got := limited.Error(); got != "https://api.example/x returned status 403 (GitHub API rate limit reached)" {
		t.Errorf("got %q, want the rate limit named", got)
	}
	if got := plain.Error(); got != "https://api.example/x returned status 403" {
		t.Errorf("got %q, want no rate limit for a plain 403", got)
	}
	if download.IsRateLimited(plain) || !download.IsRateLimited(limited) || download.IsRateLimited(errors.New("other")) {
		t.Error("IsRateLimited does not match the RateLimited flag")
	}
}
