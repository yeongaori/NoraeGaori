package mirror

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"noraegaori/internal/download"
)

const (
	sumA = "c6527f24f4b16031d3ae4fa9f658d5f11534c8d84ce7dc8502420280919c3490"
	sumB = "a0c3101b4158d1dfb7d6a78a7bf0f3de80c96bb423c152beec8beb22786f2238"
)

type fakeServer struct {
	server *httptest.Server
	files  map[string]string
	status map[string]int
}

func serveFake(t *testing.T) *fakeServer {
	t.Helper()

	fake := &fakeServer{files: map[string]string{}, status: map[string]int{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		if code, ok := fake.status[key]; ok {
			w.WriteHeader(code)
			return
		}
		body, ok := fake.files[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeServer) url(path string) string {
	return fake.server.URL + path
}

func (fake *fakeServer) publishRelease(t *testing.T, path string, release any) {
	t.Helper()

	body, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("failed to encode the release: %v", err)
	}
	fake.files[path] = string(body)
}

func (fake *fakeServer) asset(path, body string) download.Asset {
	fake.files[path] = body
	return download.Asset{Name: path[strings.LastIndex(path, "/")+1:], BrowserDownloadURL: fake.url(path)}
}

func acceptAll(string) bool { return true }

func rejectAll(string) bool { return false }

func requireNoBuild(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ErrNoBuild) {
		t.Errorf("got %v, want ErrNoBuild", err)
	}
}

func requireCheckFailure(t *testing.T, err error) {
	t.Helper()

	if err == nil || errors.Is(err, ErrNoBuild) {
		t.Errorf("got %v, want an error other than ErrNoBuild", err)
	}
}

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
	if _, err := checksumFor([]byte(sumA+"  other.zip\n"), "deno.zip"); err == nil {
		t.Error("got nil, want an error when the asset has no checksum line")
	}
	if _, err := checksumFor([]byte("not a checksum\n"), "deno.zip"); err == nil {
		t.Error("got nil, want an error for a malformed checksum file")
	}
}
