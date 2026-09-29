package mirror

import (
	"net/http"
	"testing"

	"noraegaori/internal/download"
)

const powerShellSum = "\r\nAlgorithm : SHA256\r\nHash      : A0C3101B4158D1DFB7D6A78A7BF0F3DE80C96BB423C152BEEC8BEB22786F2238\r\nPath      : C:\\a\\deno\\deno\\target\\release\\deno-x86_64-pc-windows-msvc.zip\r\n\r\n"

func publishDenoRelease(t *testing.T, fake *fakeServer, tag string, digest string) {
	t.Helper()

	linux := fake.asset("/deno/deno-x86_64-unknown-linux-gnu.zip", "linux build")
	linux.Digest = digest
	linuxSum := fake.asset("/deno/deno-x86_64-unknown-linux-gnu.zip.sha256sum", sumA+"  deno-x86_64-unknown-linux-gnu.zip\n")
	windows := fake.asset("/deno/deno-x86_64-pc-windows-msvc.zip", "windows build")
	windowsSum := fake.asset("/deno/deno-x86_64-pc-windows-msvc.zip.sha256sum", powerShellSum)
	arm := fake.asset("/deno/deno-aarch64-unknown-linux-gnu.zip", "arm build")

	fake.publishRelease(t, "/repos/denoland/deno/releases/latest", download.Release{
		TagName: tag,
		Assets:  []download.Asset{linux, linuxSum, windows, windowsSum, arm},
	})
}

func TestDenoGitHubFindsTheLinuxBuild(t *testing.T) {
	fake := serveFake(t)
	publishDenoRelease(t, fake, "v2.9.7", "sha256:"+sumA)

	found, err := DenoGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "2.9.7" || found.SHA256 != sumA || found.Archive != ZipArchive {
		t.Errorf("got %+v, want version 2.9.7 with the published sum in a zip", found)
	}
	if found.URL != fake.url("/deno/deno-x86_64-unknown-linux-gnu.zip") {
		t.Errorf("got URL %q, want the linux asset", found.URL)
	}
	if len(found.Members) != 1 || found.Members[0] != "deno" {
		t.Errorf("got members %v, want [deno]", found.Members)
	}
}

func TestDenoGitHubReadsThePowerShellChecksumOnWindows(t *testing.T) {
	fake := serveFake(t)
	publishDenoRelease(t, fake, "v2.9.7", "")

	found, err := DenoGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "windows", GOARCH: "amd64"}, acceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.SHA256 != sumB {
		t.Errorf("got sum %q, want the lower-cased PowerShell hash", found.SHA256)
	}
	if found.Members[0] != "deno.exe" {
		t.Errorf("got members %v, want [deno.exe]", found.Members)
	}
}

func TestDenoGitHubReportsNoBuild(t *testing.T) {
	fake := serveFake(t)
	publishDenoRelease(t, fake, "v2.9.7", "")
	mirror := DenoGitHub{APIURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "386"}, acceptAll)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, rejectAll)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "plan9", GOARCH: "amd64"}, acceptAll)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "riscv64"}, acceptAll)
	requireNoBuild(t, err)
}

func TestDenoGitHubRejectsAChecksumFileForAnotherAsset(t *testing.T) {
	fake := serveFake(t)
	publishDenoRelease(t, fake, "v2.9.7", "")
	fake.files["/deno/deno-x86_64-unknown-linux-gnu.zip.sha256sum"] = sumA + "  deno-aarch64-unknown-linux-gnu.zip\n"

	_, err := DenoGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireCheckFailure(t, err)
}

func TestDenoGitHubFailsClosedOnBadChecksums(t *testing.T) {
	fake := serveFake(t)
	publishDenoRelease(t, fake, "v2.9.7", "sha256:"+sumB)
	mirror := DenoGitHub{APIURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireCheckFailure(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "arm64"}, acceptAll)
	requireCheckFailure(t, err)
}

func TestDenoGitHubFailsWhenTheChecksumCannotBeFetched(t *testing.T) {
	fake := serveFake(t)
	publishDenoRelease(t, fake, "v2.9.7", "")
	fake.status["/deno/deno-x86_64-unknown-linux-gnu.zip.sha256sum"] = http.StatusBadGateway

	_, err := DenoGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireCheckFailure(t, err)
}

func TestDenoGitHubFailsWhenTheAPIFails(t *testing.T) {
	fake := serveFake(t)
	fake.status["/repos/denoland/deno/releases/latest"] = http.StatusInternalServerError

	_, err := DenoGitHub{APIURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireCheckFailure(t, err)
}

func TestDenoCDNFindsTheBuild(t *testing.T) {
	fake := serveFake(t)
	fake.files["/release-latest.txt"] = "v2.9.7\n"
	fake.files["/release/v2.9.7/deno-x86_64-pc-windows-msvc.zip.sha256sum"] = powerShellSum

	found, err := DenoCDN{BaseURL: fake.url("")}.Find(Platform{GOOS: "windows", GOARCH: "amd64"}, acceptAll)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "2.9.7" || found.SHA256 != sumB {
		t.Errorf("got %+v, want version 2.9.7 with the PowerShell hash", found)
	}
	if found.URL != fake.url("/release/v2.9.7/deno-x86_64-pc-windows-msvc.zip") {
		t.Errorf("got URL %q, want the CDN release path", found.URL)
	}
}

func TestDenoCDNReportsNoBuildForAMissingChecksum(t *testing.T) {
	fake := serveFake(t)
	fake.files["/release-latest.txt"] = "v2.9.7\n"
	mirror := DenoCDN{BaseURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "windows", GOARCH: "386"}, acceptAll)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, rejectAll)
	requireNoBuild(t, err)
}

func TestDenoCDNFailsWhenItCannotBeChecked(t *testing.T) {
	fake := serveFake(t)
	fake.files["/release-latest.txt"] = "v2.9.7\n"
	fake.status["/release/v2.9.7/deno-x86_64-unknown-linux-gnu.zip.sha256sum"] = http.StatusServiceUnavailable
	fake.files["/release/v2.9.7/deno-aarch64-unknown-linux-gnu.zip.sha256sum"] = sumA + "  other.zip\n"
	mirror := DenoCDN{BaseURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireCheckFailure(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "arm64"}, acceptAll)
	requireCheckFailure(t, err)

	fake.status["/release-latest.txt"] = http.StatusInternalServerError
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, acceptAll)
	requireCheckFailure(t, err)
}

func TestDenoChecksumForValidatesThePowerShellListing(t *testing.T) {
	cases := map[string]string{
		"another asset": "Algorithm : SHA256\r\nHash : " + sumB + "\r\nPath : C:\\x\\deno-aarch64-pc-windows-msvc.zip\r\n",
		"sha512":        "Algorithm : SHA512\r\nHash : " + sumB + "\r\nPath : C:\\x\\deno-x86_64-pc-windows-msvc.zip\r\n",
		"short hash":    "Algorithm : SHA256\r\nHash : A0C3\r\nPath : C:\\x\\deno-x86_64-pc-windows-msvc.zip\r\n",
		"no path":       "Algorithm : SHA256\r\nHash : " + sumB + "\r\n",
	}

	for name, listing := range cases {
		if _, err := denoChecksumFor([]byte(listing), "deno-x86_64-pc-windows-msvc.zip"); err == nil {
			t.Errorf("%s: got nil, want a rejection", name)
		}
	}
}
