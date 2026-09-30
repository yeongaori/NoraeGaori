package mirror_test

import (
	"net/http"
	"testing"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/download"
	"noraegaori/tests/testutil/releasetest"
)

const bunReleasesPath = "/repos/oven-sh/bun/releases?per_page=100&page="

func upTo1314(version string) bool {
	return version <= "1.3.14"
}

func bunRelease(fake *releasetest.Server, version string, withLinux bool) download.Release {
	release := download.Release{TagName: "bun-v" + version}
	sums := ""
	if withLinux {
		asset := fake.Asset("/bun/"+version+"/bun-linux-x64.zip", "bun "+version)
		asset.Digest = "sha256:" + releasetest.SumA
		release.Assets = append(release.Assets, asset)
		sums += releasetest.SumA + "  bun-linux-x64.zip\n"
	}
	windows := fake.Asset("/bun/"+version+"/bun-windows-aarch64.zip", "bun "+version)
	release.Assets = append(release.Assets, windows)
	sums += releasetest.SumB + "  bun-windows-aarch64.zip\n"
	release.Assets = append(release.Assets, fake.Asset("/bun/"+version+"/SHASUMS256.txt", sums))
	return release
}

func TestBunGitHubPicksTheNewestSupportedRelease(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.PublishRelease(t, bunReleasesPath+"1", []download.Release{
		bunRelease(fake, "1.4.2", true),
		bunRelease(fake, "1.3.14", true),
		bunRelease(fake, "1.3.13", true),
	})

	found, err := mirror.BunGitHub{APIURL: fake.URL("")}.Find(mirror.Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "1.3.14" || found.SHA256 != releasetest.SumA {
		t.Errorf("got %+v, want 1.3.14, the newest release yt-dlp supports", found)
	}
	if found.URL != fake.URL("/bun/1.3.14/bun-linux-x64.zip") || found.Members[0] != "bun-linux-x64/bun" {
		t.Errorf("got %q with %v, want the linux zip and its bun binary", found.URL, found.Members)
	}
}

func TestBunGitHubSkipsReleasesWithoutThePlatform(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.PublishRelease(t, bunReleasesPath+"1", []download.Release{
		bunRelease(fake, "1.3.14", false),
		bunRelease(fake, "1.3.13", true),
	})

	found, err := mirror.BunGitHub{APIURL: fake.URL("")}.Find(mirror.Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	if err != nil || found.Version != "1.3.13" {
		t.Errorf("got %+v, %v, want 1.3.13 because 1.3.14 has no linux build", found, err)
	}
}

func TestBunGitHubFollowsLaterPages(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.PublishRelease(t, bunReleasesPath+"1", []download.Release{bunRelease(fake, "1.4.2", true)})
	fake.PublishRelease(t, bunReleasesPath+"2", []download.Release{bunRelease(fake, "1.3.14", true)})

	found, err := mirror.BunGitHub{APIURL: fake.URL("")}.Find(mirror.Platform{GOOS: "windows", GOARCH: "arm64"}, upTo1314)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "1.3.14" || found.SHA256 != releasetest.SumB || found.Members[0] != "bun-windows-aarch64/bun.exe" {
		t.Errorf("got %+v, want 1.3.14 from the second page", found)
	}
}

func TestBunGitHubReportsNoBuild(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.PublishRelease(t, bunReleasesPath+"1", []download.Release{bunRelease(fake, "1.3.14", true)})
	fake.PublishRelease(t, bunReleasesPath+"2", []download.Release{})
	source := mirror.BunGitHub{APIURL: fake.URL("")}

	_, err := source.Find(mirror.Platform{GOOS: "linux", GOARCH: "386"}, upTo1314)
	releasetest.RequireErrorIs(t, err, mirror.ErrNoBuild)

	_, err = source.Find(mirror.Platform{GOOS: "linux", GOARCH: "riscv64"}, upTo1314)
	releasetest.RequireErrorIs(t, err, mirror.ErrNoBuild)

	_, err = source.Find(mirror.Platform{GOOS: "plan9", GOARCH: "amd64"}, upTo1314)
	releasetest.RequireErrorIs(t, err, mirror.ErrNoBuild)
}

func TestBunGitHubStopsAfterTheLastPage(t *testing.T) {
	fake := releasetest.Serve(t)
	for page := 1; page <= mirror.HookBunMaximumPages; page++ {
		fake.PublishRelease(t, bunReleasesPath+string(rune('0'+page)), []download.Release{bunRelease(fake, "1.4.2", true)})
	}
	fake.Status[bunReleasesPath+"6"] = http.StatusInternalServerError

	_, err := mirror.BunGitHub{APIURL: fake.URL("")}.Find(mirror.Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	releasetest.RequireErrorIs(t, err, mirror.ErrNoBuild)
}

func TestBunGitHubFailsWhenItCannotBeChecked(t *testing.T) {
	fake := releasetest.Serve(t)
	release := bunRelease(fake, "1.3.14", true)
	release.Assets[0].Digest = "sha256:" + releasetest.SumB
	fake.PublishRelease(t, bunReleasesPath+"1", []download.Release{release})
	source := mirror.BunGitHub{APIURL: fake.URL("")}

	_, err := source.Find(mirror.Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	releasetest.RequireOtherError(t, err, mirror.ErrNoBuild)

	release.Assets = release.Assets[:2]
	fake.PublishRelease(t, bunReleasesPath+"1", []download.Release{release})
	_, err = source.Find(mirror.Platform{GOOS: "windows", GOARCH: "arm64"}, upTo1314)
	releasetest.RequireOtherError(t, err, mirror.ErrNoBuild)

	fake.Status[bunReleasesPath+"1"] = http.StatusForbidden
	_, err = source.Find(mirror.Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	releasetest.RequireOtherError(t, err, mirror.ErrNoBuild)
}
