package mirror_test

import (
	"testing"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/tests/testutil/releasetest"
)

func TestPlatformNamesExecutables(t *testing.T) {
	if got := (mirror.Platform{GOOS: "windows", GOARCH: "386"}).Executable("deno"); got != "deno.exe" {
		t.Errorf("got %q, want deno.exe on windows", got)
	}
	if got := (mirror.Platform{GOOS: "linux", GOARCH: "arm64"}).Executable("deno"); got != "deno" {
		t.Errorf("got %q, want deno on linux", got)
	}
	if got := (mirror.Platform{GOOS: "linux", GOARCH: "arm64"}).String(); got != "linux/arm64" {
		t.Errorf("got %q, want linux/arm64", got)
	}
}

func TestMirrorsNameTheirSource(t *testing.T) {
	names := map[string]mirror.Mirror{
		"GitHub denoland/deno":        mirror.DenoGitHub{},
		"dl.deno.land":                mirror.DenoCDN{},
		"nodejs.org":                  mirror.NodeDist{},
		"GitHub oven-sh/bun":          mirror.BunGitHub{},
		"GitHub yt-dlp/FFmpeg-Builds": mirror.FFmpegGitHub{Repo: "yt-dlp/FFmpeg-Builds"},
	}

	for want, m := range names {
		if got := m.Name(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestChecksumForRejectsAMissingEntry(t *testing.T) {
	if _, err := mirror.HookChecksumFor([]byte(releasetest.SumA+"  other.zip\n"), "deno.zip"); err == nil {
		t.Error("got nil, want an error when the asset has no checksum line")
	}
	if _, err := mirror.HookChecksumFor([]byte("not a checksum\n"), "deno.zip"); err == nil {
		t.Error("got nil, want an error for a malformed checksum file")
	}
}
