package mirror_test

import (
	"testing"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/tests/testutil/releasetest"
)

func TestFFmpegGitHubFallsBackToGitHubWeb(t *testing.T) {
	api, web := releasetest.Serve(t), releasetest.Serve(t)
	repo := "yt-dlp/FFmpeg-Builds"
	api.RateLimit("/repos/" + repo + "/releases/latest")
	web.RedirectLatest(repo, "latest")
	web.Files["/"+repo+"/releases/download/latest/checksums.sha256"] = releasetest.SumA + "  ffmpeg-master-latest-linux64-gpl.tar.xz\n" + releasetest.SumB + "  ffmpeg-master-latest-win64-gpl.zip\n"
	web.PublishFeed(repo,
		releasetest.FeedEntry{Tag: "autobuild-2026-09-24-10-00", Updated: "2026-09-24T10:00:00Z"},
		releasetest.FeedEntry{Tag: "latest", Updated: "2026-09-25T18:45:57Z"},
	)

	found, err := mirror.FFmpegGitHub{APIURL: api.URL(""), WebURL: web.URL(""), Repo: repo}.Find(mirror.Platform{GOOS: "windows", GOARCH: "amd64"}, releasetest.AcceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want the build found through github.com", err)
	}
	if found.Version != "2026.09.25.1845" {
		t.Errorf("got version %q, want the one the API would report, from the tag's feed entry", found.Version)
	}
	if found.URL != web.URL("/"+repo+"/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip") || found.SHA256 != releasetest.SumB {
		t.Errorf("got %q with %s, want the github.com download and the hash from the checksum file", found.URL, found.SHA256)
	}
}

func TestFFmpegGitHubWithoutAFeedCannotDateTheBuild(t *testing.T) {
	api, web := releasetest.Serve(t), releasetest.Serve(t)
	repo := "BtbN/FFmpeg-Builds"
	api.RateLimit("/repos/" + repo + "/releases/latest")
	web.RedirectLatest(repo, "latest")
	web.Files["/"+repo+"/releases/download/latest/checksums.sha256"] = releasetest.SumA + "  ffmpeg-master-latest-linux64-gpl.tar.xz\n"

	_, err := mirror.FFmpegGitHub{APIURL: api.URL(""), WebURL: web.URL(""), Repo: repo}.Find(mirror.Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, mirror.ErrNoBuild)
}
