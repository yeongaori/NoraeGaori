package mirror

import (
	"net/http"
	"testing"

	"noraegaori/internal/download"
	"noraegaori/tests/testutil/releasetest"
)

const powerShellSum = "\r\nAlgorithm : SHA256\r\nHash      : A0C3101B4158D1DFB7D6A78A7BF0F3DE80C96BB423C152BEEC8BEB22786F2238\r\nPath      : C:\\a\\deno\\deno\\target\\release\\deno-x86_64-pc-windows-msvc.zip\r\n\r\n"

func publishDenoRelease(t *testing.T, fake *releasetest.Server, tag string, digest string) {
	t.Helper()

	linux := fake.Asset("/deno/deno-x86_64-unknown-linux-gnu.zip", "linux build")
	linux.Digest = digest
	linuxSum := fake.Asset("/deno/deno-x86_64-unknown-linux-gnu.zip.sha256sum", releasetest.SumA+"  deno-x86_64-unknown-linux-gnu.zip\n")
	windows := fake.Asset("/deno/deno-x86_64-pc-windows-msvc.zip", "windows build")
	windowsSum := fake.Asset("/deno/deno-x86_64-pc-windows-msvc.zip.sha256sum", powerShellSum)
	arm := fake.Asset("/deno/deno-aarch64-unknown-linux-gnu.zip", "arm build")

	fake.PublishRelease(t, "/repos/denoland/deno/releases/latest", download.Release{
		TagName: tag,
		Assets:  []download.Asset{linux, linuxSum, windows, windowsSum, arm},
	})
}

func TestDenoGitHubFindsTheLinuxBuild(t *testing.T) {
	fake := releasetest.Serve(t)
	publishDenoRelease(t, fake, "v2.9.7", "sha256:"+releasetest.SumA)

	found, err := DenoGitHub{APIURL: fake.URL("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "2.9.7" || found.SHA256 != releasetest.SumA || found.Archive != ZipArchive {
		t.Errorf("got %+v, want version 2.9.7 with the published sum in a zip", found)
	}
	if found.URL != fake.URL("/deno/deno-x86_64-unknown-linux-gnu.zip") {
		t.Errorf("got URL %q, want the linux asset", found.URL)
	}
	if len(found.Members) != 1 || found.Members[0] != "deno" {
		t.Errorf("got members %v, want [deno]", found.Members)
	}
}

func TestDenoGitHubReadsThePowerShellChecksumOnWindows(t *testing.T) {
	fake := releasetest.Serve(t)
	publishDenoRelease(t, fake, "v2.9.7", "")

	found, err := DenoGitHub{APIURL: fake.URL("")}.Find(Platform{GOOS: "windows", GOARCH: "amd64"}, releasetest.AcceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.SHA256 != releasetest.SumB {
		t.Errorf("got sum %q, want the lower-cased PowerShell hash", found.SHA256)
	}
	if found.Members[0] != "deno.exe" {
		t.Errorf("got members %v, want [deno.exe]", found.Members)
	}
}

func TestDenoGitHubReportsNoBuild(t *testing.T) {
	fake := releasetest.Serve(t)
	publishDenoRelease(t, fake, "v2.9.7", "")
	mirror := DenoGitHub{APIURL: fake.URL("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "386"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.RejectAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "plan9", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "riscv64"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)
}

func TestDenoGitHubRejectsAChecksumFileForAnotherAsset(t *testing.T) {
	fake := releasetest.Serve(t)
	publishDenoRelease(t, fake, "v2.9.7", "")
	fake.Files["/deno/deno-x86_64-unknown-linux-gnu.zip.sha256sum"] = releasetest.SumA + "  deno-aarch64-unknown-linux-gnu.zip\n"

	_, err := DenoGitHub{APIURL: fake.URL("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)
}

func TestDenoGitHubFailsClosedOnBadChecksums(t *testing.T) {
	fake := releasetest.Serve(t)
	publishDenoRelease(t, fake, "v2.9.7", "sha256:"+releasetest.SumB)
	mirror := DenoGitHub{APIURL: fake.URL("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "arm64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)
}

func TestDenoGitHubFailsWhenTheChecksumCannotBeFetched(t *testing.T) {
	fake := releasetest.Serve(t)
	publishDenoRelease(t, fake, "v2.9.7", "")
	fake.Status["/deno/deno-x86_64-unknown-linux-gnu.zip.sha256sum"] = http.StatusBadGateway

	_, err := DenoGitHub{APIURL: fake.URL("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)
}

func TestDenoGitHubFailsWhenTheAPIFails(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.Status["/repos/denoland/deno/releases/latest"] = http.StatusInternalServerError

	_, err := DenoGitHub{APIURL: fake.URL("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)
}

func TestDenoCDNFindsTheBuild(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.Files["/release-latest.txt"] = "v2.9.7\n"
	fake.Files["/release/v2.9.7/deno-x86_64-pc-windows-msvc.zip.sha256sum"] = powerShellSum

	found, err := DenoCDN{BaseURL: fake.URL("")}.Find(Platform{GOOS: "windows", GOARCH: "amd64"}, releasetest.AcceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "2.9.7" || found.SHA256 != releasetest.SumB {
		t.Errorf("got %+v, want version 2.9.7 with the PowerShell hash", found)
	}
	if found.URL != fake.URL("/release/v2.9.7/deno-x86_64-pc-windows-msvc.zip") {
		t.Errorf("got URL %q, want the CDN release path", found.URL)
	}
}

func TestDenoCDNReportsNoBuildForAMissingChecksum(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.Files["/release-latest.txt"] = "v2.9.7\n"
	mirror := DenoCDN{BaseURL: fake.URL("")}

	_, err := mirror.Find(Platform{GOOS: "windows", GOARCH: "386"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.RejectAll)
	releasetest.RequireErrorIs(t, err, ErrNoBuild)
}

func TestDenoCDNFailsWhenItCannotBeChecked(t *testing.T) {
	fake := releasetest.Serve(t)
	fake.Files["/release-latest.txt"] = "v2.9.7\n"
	fake.Status["/release/v2.9.7/deno-x86_64-unknown-linux-gnu.zip.sha256sum"] = http.StatusServiceUnavailable
	fake.Files["/release/v2.9.7/deno-aarch64-unknown-linux-gnu.zip.sha256sum"] = releasetest.SumA + "  other.zip\n"
	mirror := DenoCDN{BaseURL: fake.URL("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "arm64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)

	fake.Status["/release-latest.txt"] = http.StatusInternalServerError
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, releasetest.AcceptAll)
	releasetest.RequireOtherError(t, err, ErrNoBuild)
}

func TestDenoChecksumForValidatesThePowerShellListing(t *testing.T) {
	cases := map[string]string{
		"another asset": "Algorithm : SHA256\r\nHash : " + releasetest.SumB + "\r\nPath : C:\\x\\deno-aarch64-pc-windows-msvc.zip\r\n",
		"sha512":        "Algorithm : SHA512\r\nHash : " + releasetest.SumB + "\r\nPath : C:\\x\\deno-x86_64-pc-windows-msvc.zip\r\n",
		"short hash":    "Algorithm : SHA256\r\nHash : A0C3\r\nPath : C:\\x\\deno-x86_64-pc-windows-msvc.zip\r\n",
		"no path":       "Algorithm : SHA256\r\nHash : " + releasetest.SumB + "\r\n",
	}

	for name, listing := range cases {
		if _, err := denoChecksumFor([]byte(listing), "deno-x86_64-pc-windows-msvc.zip"); err == nil {
			t.Errorf("%s: got nil, want a rejection", name)
		}
	}
}
