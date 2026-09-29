package mirror

import (
	"net/http"
	"testing"
)

const nodeIndex = `[
	{"version":"v26.10.0","files":["linux-x64","win-x64-zip","win-x86-zip"],"lts":false},
	{"version":"v24.21.0","files":["linux-arm64","linux-x64","win-x64-zip"],"lts":"Krypton"},
	{"version":"v22.23.3","files":["linux-x64","win-x64-zip","win-x86-zip"],"lts":"Jod"},
	{"version":"v20.19.0","files":["linux-x86","win-x86-zip"],"lts":"Iron"}
]`

func atLeast22(version string) bool {
	return version >= "22"
}

func serveNode(t *testing.T) *fakeServer {
	t.Helper()

	fake := serveFake(t)
	fake.files["/index.json"] = nodeIndex
	fake.files["/v24.21.0/SHASUMS256.txt"] = sumA + "  node-v24.21.0-linux-x64.tar.xz\n" + sumB + "  node-v24.21.0-win-x64.zip\n"
	fake.files["/v22.23.3/SHASUMS256.txt"] = sumB + "  node-v22.23.3-win-x86.zip\n"
	return fake
}

func TestNodeDistPicksTheNewestLTSForLinux(t *testing.T) {
	fake := serveNode(t)

	found, err := NodeDist{BaseURL: fake.url("")}.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, atLeast22)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "24.21.0" || found.SHA256 != sumA || found.Archive != TarXzArchive {
		t.Errorf("got %+v, want the 24.21.0 LTS tarball, skipping the newer non-LTS 26", found)
	}
	if found.URL != fake.url("/v24.21.0/node-v24.21.0-linux-x64.tar.xz") {
		t.Errorf("got URL %q, want the linux tarball", found.URL)
	}
	if len(found.Members) != 1 || found.Members[0] != "node-v24.21.0-linux-x64/bin/node" {
		t.Errorf("got members %v, want the bin/node path", found.Members)
	}
}

func TestNodeDistSkipsReleasesWithoutThePlatform(t *testing.T) {
	fake := serveNode(t)

	found, err := NodeDist{BaseURL: fake.url("")}.Find(Platform{GOOS: "windows", GOARCH: "386"}, atLeast22)
	if err != nil {
		t.Fatalf("Find returned %v, want nil", err)
	}
	if found.Version != "22.23.3" || found.SHA256 != sumB || found.Archive != ZipArchive {
		t.Errorf("got %+v, want 22.23.3, the newest LTS with a win-x86 zip", found)
	}
	if found.Members[0] != "node-v22.23.3-win-x86/node.exe" {
		t.Errorf("got members %v, want node.exe in the zip folder", found.Members)
	}
}

func TestNodeDistReportsNoBuild(t *testing.T) {
	fake := serveNode(t)
	mirror := NodeDist{BaseURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "386"}, atLeast22)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "riscv64"}, atLeast22)
	requireNoBuild(t, err)

	_, err = mirror.Find(Platform{GOOS: "plan9", GOARCH: "amd64"}, atLeast22)
	requireNoBuild(t, err)
}

func TestNodeDistFailsWhenItCannotBeChecked(t *testing.T) {
	fake := serveNode(t)
	delete(fake.files, "/v24.21.0/SHASUMS256.txt")
	mirror := NodeDist{BaseURL: fake.url("")}

	_, err := mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, atLeast22)
	requireCheckFailure(t, err)

	fake.files["/v24.21.0/SHASUMS256.txt"] = sumA + "  node-v24.21.0-darwin-x64.tar.gz\n"
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, atLeast22)
	requireCheckFailure(t, err)

	fake.status["/index.json"] = http.StatusInternalServerError
	_, err = mirror.Find(Platform{GOOS: "linux", GOARCH: "amd64"}, atLeast22)
	requireCheckFailure(t, err)
}
