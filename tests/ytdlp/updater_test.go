package ytdlp_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/config"
	"noraegaori/internal/ytdlp"
	"noraegaori/tests/testutil"
)

func TestExpectedChecksumAgrees(t *testing.T) {
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

func TestExpectedChecksumNoSource(t *testing.T) {
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

func TestPickAssetStandalone(t *testing.T) {
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

	for i := range cases {
		c := &cases[i]
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

func TestPickAssetNewStandalone(t *testing.T) {
	release := releaseWithAssets(append(publishedAssetNames, "yt-dlp_linux_i686")...)

	got, err := ytdlp.HookPickAsset(release, "linux", "386")
	if err != nil || got.Name != "yt-dlp_linux_i686" {
		t.Errorf("got %v, %v, want the new standalone linux/386 build", got, err)
	}
}

func TestPickAssetPythonFallback(t *testing.T) {
	release := releaseWithAssets("yt-dlp", "yt-dlp.exe")

	got, err := ytdlp.HookPickAsset(release, "linux", "amd64")
	if err != nil || got.Name != "yt-dlp" {
		t.Errorf("got %v, %v, want the Python build when the standalone one is missing", got, err)
	}
}

func TestPickAssetNoBuild(t *testing.T) {
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

func TestUpdateFromChannelPending(t *testing.T) {
	versionmanager := useVersionManager(t)
	stubLatestVersion(t, "2026.08.19")
	stubCanary(t, false, true)

	addVersion(versionmanager, "2026.08.19", &ytdlp.VersionEntry{Path: writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.08.19"), "echo 2026.08.19"), State: ytdlp.StatePending})
	addVersion(versionmanager, "2026.08.18.122307", &ytdlp.VersionEntry{Path: "lib/n/yt-dlp", State: ytdlp.StateActive, Successes: 40})
	versionmanager.HookState().ActiveVersion = "2026.08.18.122307"

	outcome, err := ytdlp.HookUpdateFromChannel(config.YtDlpChannelStable, false)
	if err != nil {
		t.Fatalf("updateFromChannel returned %v, want nil", err)
	}
	if *outcome.HookUpdated() {
		t.Error("got updated=true, want false while the canary cannot reach YouTube")
	}
}

func TestUpdateFromChannelFallback(t *testing.T) {
	versionmanager := useVersionManager(t)
	stubLatestVersion(t, "2026.08.19")

	addVersion(versionmanager, "2026.08.19", &ytdlp.VersionEntry{Path: "lib/b/yt-dlp", State: ytdlp.StateBlacklisted})
	testutil.Swap(t, ytdlp.HookGetReleasesFn, func(channel string, perPage int) ([]*ytdlp.GitHubRelease, error) {
		return nil, errors.New("release list unavailable")
	})

	if _, err := ytdlp.HookUpdateFromChannel(config.YtDlpChannelStable, false); err == nil {
		t.Error("updateFromChannel returned nil, want the fallback install failure")
	}
}
