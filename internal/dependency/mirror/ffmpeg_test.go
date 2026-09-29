package mirror

import (
	"net/http"
	"testing"

	"noraegaori/internal/download"
	"noraegaori/tests/testutil/releasetest"
)

func publishFFmpegRelease(t *testing.T, fake *releasetest.Server, repo, published string) {
	t.Helper()

	linux := fake.Asset("/"+repo+"/ffmpeg-master-latest-linux64-gpl.tar.xz", "linux build")
	linux.Digest = "sha256:" + releasetest.SumA
	windows := fake.Asset("/"+repo+"/ffmpeg-master-latest-win64-gpl.zip", "windows build")
	sums := fake.Asset("/"+repo+"/checksums.sha256", releasetest.SumA+"  ffmpeg-master-latest-linux64-gpl.tar.xz\n"+releasetest.SumB+"  ffmpeg-master-latest-win64-gpl.zip\n")

	fake.PublishRelease(t, "/repos/"+repo+"/releases/latest", download.Release{
		TagName:     "latest",
		PublishedAt: published,
		Assets:      []download.Asset{linux, windows, sums},
	})
}

func TestFFmpegGitHubFindsTheLinuxTarball(t *testing.T) {
	fake := releasetest.Serve(t)
	publishFFmpegRelease(t, fake, "yt-dlp/FFmpeg-Builds", "2026-09-25T18:45:57Z")

	found, err := FFmpegGitHub{APIURL: fake.URL(""), Repo: "yt-dlp/FFmpeg-Builds"}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "2026.09.25.1845" || found.SHA256 != releasetest.SumA || found.Archive != TarXzArchive {
		t.Errorf("got %+v, want the build dated 2026.09.25.1845 as a tarball", found)
	}
	want := []string{"ffmpeg-master-latest-linux64-gpl/bin/ffmpeg", "ffmpeg-master-latest-linux64-gpl/bin/ffprobe"}
	if len(found.Members) != 2 || found.Members[0] != want[0] || found.Members[1] != want[1] {
		t.Errorf("got members %v, want %v", found.Members, want)
	}
}

func TestFFmpegGitHubFindsTheWindowsZip(t *testing.T) {
	fake := releasetest.Serve(t)
	publishFFmpegRelease(t, fake, "BtbN/FFmpeg-Builds", "2026-09-27T13:23:55+09:00")
	mirror := FFmpegGitHub{APIURL: fake.URL(""), Repo: "BtbN/FFmpeg-Builds"}

	found, err := mirror.Find(Platform{GOOS: "windows", GOARCH: "amd64"}, releasetest.AcceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "2026.09.27.0423" || found.SHA256 != releasetest.SumB || found.Archive != ZipArchive {
		t.Errorf("got %+v, want the UTC-dated windows zip", found)
	}
	if found.Members[0] != "ffmpeg-master-latest-win64-gpl/bin/ffmpeg.exe" || found.Members[1] != "ffmpeg-master-latest-win64-gpl/bin/ffprobe.exe" {
		t.Errorf("got members %v, want the .exe binaries", found.Members)
	}
	if mirror.Name() != "GitHub BtbN/FFmpeg-Builds" {
		t.Errorf("got name %q, want the repository named", mirror.Name())
	}
}

func TestFFmpegGitHubReportsNoBuild(t *testing.T) {
	fake := releasetest.Serve(t)
	publishFFmpegRelease(t, fake, "BtbN/FFmpeg-Builds", "2026-09-27T13:23:55Z")
	mirror := FFmpegGitHub{APIURL: fake.URL(""), Repo: "BtbN/FFmpeg-Builds"}

	_, err := mirror.Find(Platform{GOOS: "windows", GOARCH: "386"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "darwin", GOARCH: "arm64"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.RejectAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)
}

func TestFFmpegGitHubFailsWhenItCannotBeChecked(t *testing.T) {
	fake := releasetest.Serve(t)
	publishFFmpegRelease(t, fake, "yt-dlp/FFmpeg-Builds", "yesterday")
	mirror := FFmpegGitHub{APIURL: fake.URL(""), Repo: "yt-dlp/FFmpeg-Builds"}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)

	publishFFmpegRelease(t, fake, "yt-dlp/FFmpeg-Builds", "2026-09-25T18:45:57Z")
	fake.Status["/yt-dlp/FFmpeg-Builds/checksums.sha256"] = http.StatusNotFound
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)

	fake.Status["/repos/yt-dlp/FFmpeg-Builds/releases/latest"] = http.StatusInternalServerError
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)
}
