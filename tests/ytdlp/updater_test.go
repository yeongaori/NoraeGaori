package ytdlp_test

import (
	"strings"
	"testing"

	"noraegaori/internal/ytdlp"
)

func TestExpectedChecksumAcceptsAnAgreeingDigest(t *testing.T) {
	payload := []byte("yt-dlp binary")
	release, _ := serveRelease(t, payload, sha256Hex(payload))
	release.Assets[0].Digest = "sha256:" + strings.ToUpper(sha256Hex(payload))

	sum, err := ytdlp.ExpectedChecksum(release, testAssetName)
	if err != nil {
		t.Fatalf("ExpectedChecksum returned %v, want nil", err)
	}
	if sum != sha256Hex(payload) {
		t.Errorf("got %q, want %q", sum, sha256Hex(payload))
	}
}

func TestExpectedChecksumFailsClosedWithoutSource(t *testing.T) {
	release := &ytdlp.GitHubRelease{TagName: "2026.07.04"}

	if _, err := ytdlp.ExpectedChecksum(release, "yt-dlp"); err == nil {
		t.Error("got nil error, want a failure when neither a digest nor a sums asset exists")
	}
}

var publishedAssetNames = []string{
	"yt-dlp", "yt-dlp.exe", "yt-dlp_x86.exe", "yt-dlp_arm64.exe",
	"yt-dlp_linux", "yt-dlp_linux_aarch64", "yt-dlp_macos", "yt-dlp_musllinux",
}

func releaseWithAssets(names ...string) *ytdlp.GitHubRelease {
	release := &ytdlp.GitHubRelease{TagName: "2026.07.04"}
	for _, name := range names {
		addAsset(release, name, "https://example.invalid/"+name, "")
	}
	return release
}

func TestPickAssetChoosesTheStandaloneBuildForEachPlatform(t *testing.T) {
	release := releaseWithAssets(publishedAssetNames...)
	cases := []struct{ goos, goarch, want string }{
		{"linux", "amd64", "yt-dlp_linux"},
		{"linux", "arm64", "yt-dlp_linux_aarch64"},
		{"linux", "386", "yt-dlp"},
		{"windows", "amd64", "yt-dlp.exe"},
		{"windows", "386", "yt-dlp_x86.exe"},
		{"windows", "arm64", "yt-dlp_arm64.exe"},
		{"darwin", "arm64", "yt-dlp_macos"},
		{"linux", "arm", "yt-dlp"},
	}

	for _, c := range cases {
		got, err := ytdlp.HookPickAsset(release, c.goos, c.goarch)
		if err != nil {
			t.Errorf("%s/%s: got error %v, want %q", c.goos, c.goarch, err, c.want)
			continue
		}
		if got.Name != c.want || got.BrowserDownloadURL != "https://example.invalid/"+c.want {
			t.Errorf("%s/%s: got %q at %q, want %q", c.goos, c.goarch, got.Name, got.BrowserDownloadURL, c.want)
		}
	}
}

func TestPickAssetUsesAStandaloneBuildOnceItIsPublished(t *testing.T) {
	release := releaseWithAssets(append(publishedAssetNames, "yt-dlp_linux_i686")...)

	got, err := ytdlp.HookPickAsset(release, "linux", "386")
	if err != nil || got.Name != "yt-dlp_linux_i686" {
		t.Errorf("got %v, %v, want the new standalone linux/386 build", got, err)
	}
}

func TestPickAssetFallsBackToThePythonBuild(t *testing.T) {
	release := releaseWithAssets("yt-dlp", "yt-dlp.exe")

	got, err := ytdlp.HookPickAsset(release, "linux", "amd64")
	if err != nil || got.Name != "yt-dlp" {
		t.Errorf("got %v, %v, want the Python build when the standalone one is missing", got, err)
	}
}

func TestPickAssetRejectsPlatformsWithoutABuild(t *testing.T) {
	cases := map[string]struct {
		release      *ytdlp.GitHubRelease
		goos, goarch string
	}{
		"windows without its exe":   {releaseWithAssets("yt-dlp", "yt-dlp.exe"), "windows", "arm64"},
		"windows never uses python": {releaseWithAssets("yt-dlp"), "windows", "amd64"},
		"empty release":             {releaseWithAssets(), "linux", "amd64"},
	}

	for name, c := range cases {
		if got, err := ytdlp.HookPickAsset(c.release, c.goos, c.goarch); err == nil {
			t.Errorf("%s: got %v, want an error", name, got)
		}
	}
}
