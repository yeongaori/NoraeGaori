package mirror

import (
	"testing"

	"noraegaori/tests/testutil/releasetest"
)

func TestPlatformNamesExecutables(t *testing.T) {
	if got := (Platform{GOOS: "windows", GOARCH: "386"}).Executable("deno"); got != "deno.exe" {
		t.Errorf("got %q, want deno.exe on windows", got)
	}
	if got := (Platform{GOOS: "linux", GOARCH: "arm64"}).Executable("deno"); got != "deno" {
		t.Errorf("got %q, want deno on linux", got)
	}
	if got := (Platform{GOOS: "linux", GOARCH: "arm64"}).String(); got != "linux/arm64" {
		t.Errorf("got %q, want linux/arm64", got)
	}
}

func TestMirrorsNameTheirSource(t *testing.T) {
	names := map[string]Mirror{
		"GitHub denoland/deno":        DenoGitHub{},
		"dl.deno.land":                DenoCDN{},
		"nodejs.org":                  NodeDist{},
		"GitHub oven-sh/bun":          BunGitHub{},
		"GitHub yt-dlp/FFmpeg-Builds": FFmpegGitHub{Repo: "yt-dlp/FFmpeg-Builds"},
	}

	for want, m := range names {
		if got := m.Name(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestChecksumForRejectsAMissingEntry(t *testing.T) {
	if _, err := checksumFor([]byte(releasetest.SumA+"  other.zip\n"), "deno.zip"); err == nil {
		t.Error("got nil, want an error when the asset has no checksum line")
	}
	if _, err := checksumFor([]byte("not a checksum\n"), "deno.zip"); err == nil {
		t.Error("got nil, want an error for a malformed checksum file")
	}
}
