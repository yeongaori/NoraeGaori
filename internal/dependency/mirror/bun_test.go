package mirror

import (
	"net/http"
	"testing"

	"noraegaori/internal/download"
)

const bunReleasesPath = "/repos/oven-sh/bun/releases?per_page=100&page="

func upTo1314(version string) bool {
	return version <= "1.3.14"
}

func bunRelease(fake *fakeServer, version string, withLinux bool) download.Release {
	release := download.Release{TagName: "bun-v" + version}
	sums := ""
	if withLinux {
		asset := fake.asset("/bun/"+version+"/bun-linux-x64.zip", "bun "+version)
		asset.Digest = "sha256:" + sumA
		release.Assets = append(release.Assets, asset)
		sums += sumA + "  bun-linux-x64.zip\n"
	}
	windows := fake.asset("/bun/"+version+"/bun-windows-aarch64.zip", "bun "+version)
	release.Assets = append(release.Assets, windows)
	sums += sumB + "  bun-windows-aarch64.zip\n"
	release.Assets = append(release.Assets, fake.asset("/bun/"+version+"/SHASUMS256.txt", sums))
	return release
}

func TestBunGitHubPicksTheNewestSupportedRelease(t *testing.T) {
	fake := serveFake(t)
	fake.publishRelease(t, bunReleasesPath+"1", []download.Release{
		bunRelease(fake, "1.4.2", true),
		bunRelease(fake, "1.3.14", true),
		bunRelease(fake, "1.3.13", true),
	})

	found, err := BunGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "1.3.14" || found.SHA256 != sumA {
		t.Errorf("got %+v, want 1.3.14, the newest release yt-dlp supports", found)
	}
	if found.URL != fake.url("/bun/1.3.14/bun-linux-x64.zip") || found.Members[0] != "bun-linux-x64/bun" {
		t.Errorf("got %q with %v, want the linux zip and its bun binary", found.URL, found.Members)
	}
}

func TestBunGitHubSkipsReleasesWithoutThePlatform(t *testing.T) {
	fake := serveFake(t)
	fake.publishRelease(t, bunReleasesPath+"1", []download.Release{
		bunRelease(fake, "1.3.14", false),
		bunRelease(fake, "1.3.13", true),
	})

	found, err := BunGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	if err != nil || found.Version != "1.3.13" {
		t.Errorf("got %+v, %v, want 1.3.13 because 1.3.14 has no linux build", found, err)
	}
}

func TestBunGitHubFollowsLaterPages(t *testing.T) {
	fake := serveFake(t)
	fake.publishRelease(t, bunReleasesPath+"1", []download.Release{bunRelease(fake, "1.4.2", true)})
	fake.publishRelease(t, bunReleasesPath+"2", []download.Release{bunRelease(fake, "1.3.14", true)})

	found, err := BunGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "windows", GOARCH: "arm64"}, upTo1314)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "1.3.14" || found.SHA256 != sumB || found.Members[0] != "bun-windows-aarch64/bun.exe" {
		t.Errorf("got %+v, want 1.3.14 from the second page", found)
	}
}

func TestBunGitHubReportsNoBuild(t *testing.T) {
	fake := serveFake(t)
	fake.publishRelease(t, bunReleasesPath+"1", []download.Release{bunRelease(fake, "1.3.14", true)})
	fake.publishRelease(t, bunReleasesPath+"2", []download.Release{})
	mirror := BunGitHub{APIURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "386"}, upTo1314)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "riscv64"}, upTo1314)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "plan9", GOARCH: "amd64"}, upTo1314)
	requireNoBuild(t, err)
}

func TestBunGitHubStopsAfterTheLastPage(t *testing.T) {
	fake := serveFake(t)
	for page := 1; page <= bunMaximumPages; page++ {
		fake.publishRelease(t, bunReleasesPath+string(rune('0'+page)), []download.Release{bunRelease(fake, "1.4.2", true)})
	}
	fake.status[bunReleasesPath+"6"] = http.StatusInternalServerError

	_, err := BunGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	requireNoBuild(t, err)
}

func TestBunGitHubFailsWhenItCannotBeChecked(t *testing.T) {
	fake := serveFake(t)
	release := bunRelease(fake, "1.3.14", true)
	release.Assets[0].Digest = "sha256:" + sumB
	fake.publishRelease(t, bunReleasesPath+"1", []download.Release{release})
	mirror := BunGitHub{APIURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	requireCheckFailure(t, err)

	release.Assets = release.Assets[:2]
	fake.publishRelease(t, bunReleasesPath+"1", []download.Release{release})
	_, err = mirror.Find(Platform{GOOS: "windows", GOARCH: "arm64"}, upTo1314)
	requireCheckFailure(t, err)

	fake.status[bunReleasesPath+"1"] = http.StatusForbidden
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, upTo1314)
	requireCheckFailure(t, err)
}
