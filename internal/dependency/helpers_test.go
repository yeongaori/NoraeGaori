package dependency

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/logger"
	"noraegaori/tests/testutil"

	"github.com/ulikunitz/xz"
)

var linuxAmd64 = mirror.Platform{GOOS: "linux", GOARCH: "amd64"}

func skipOnWindows(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake executables are not portable to windows")
	}
}

func script(output string) string {
	return "#!/bin/sh\necho '" + output + "'\n"
}

const failingScript = "#!/bin/sh\nexit 1\n"

func writeExecutable(t *testing.T, path, content string) string {
	t.Helper()

	skipOnWindows(t)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
	return path
}

func useWorkingDirectory(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
}

func usePath(t *testing.T, directories ...string) {
	t.Helper()

	path := t.TempDir()
	for _, directory := range directories {
		path = directory + string(os.PathListSeparator) + path
	}
	t.Setenv("PATH", path)
	t.Setenv("HOME", t.TempDir())
}

func resetState(t *testing.T) {
	t.Helper()

	previousFFmpeg := ffmpegSlot.active.Swap(nil)
	previousJsRuntime := jsRuntimeSlot.active.Swap(nil)
	retiredMu.Lock()
	previousRetired := retiredBinaries
	retiredBinaries = nil
	retiredMu.Unlock()

	t.Cleanup(func() {
		ffmpegSlot.active.Store(previousFFmpeg)
		jsRuntimeSlot.active.Store(previousJsRuntime)
		retiredMu.Lock()
		retiredBinaries = previousRetired
		retiredMu.Unlock()
	})
}

func captureLog(t *testing.T) func() string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.log")
	logger.SetLogFile(path)
	t.Cleanup(func() { logger.SetLogFile("") })

	return func() string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read the captured log: %v", err)
		}
		return string(content)
	}
}

type stubMirror struct {
	label string
	found *mirror.Candidate
	err   error
	calls atomic.Int32
}

func (m *stubMirror) Name() string {
	return m.label
}

func (m *stubMirror) Find(target mirror.Platform, accepts func(string) bool) (*mirror.Candidate, error) {
	m.calls.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	if !accepts(m.found.Version) {
		return nil, mirror.ErrNoBuild
	}
	return m.found, nil
}

func noBuildMirror(label string) *stubMirror {
	return &stubMirror{label: label, err: mirror.ErrNoBuild}
}

func unreachableMirror(label string) *stubMirror {
	return &stubMirror{label: label, err: errors.New(label + " is unreachable")}
}

func stubTool(name string, mirrors ...mirror.Mirror) *tool {
	return &tool{name: name, displayName: name, versionFlag: "--version", mirrors: mirrors}
}

func useJsRuntimes(t *testing.T, tools ...*tool) {
	t.Helper()

	testutil.Swap(t, &jsRuntimes, tools)
}

func useFFmpegTool(t *testing.T, mirrors ...mirror.Mirror) {
	t.Helper()

	testutil.Swap(t, &ffmpegTool, &tool{name: "ffmpeg", displayName: "ffmpeg", versionFlag: "-version", mirrors: mirrors})
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		content := files[name]
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("failed to add %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close the zip: %v", err)
	}
	return buffer.Bytes()
}

func buildTarXz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	compressor, err := xz.NewWriter(&buffer)
	if err != nil {
		t.Fatalf("failed to start xz: %v", err)
	}
	writer := tar.NewWriter(compressor)
	if err := writer.WriteHeader(&tar.Header{Name: "bundle/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
		t.Fatalf("failed to add the directory: %v", err)
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		content := files[name]
		header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(content))}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("failed to add %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close the tar: %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("failed to close xz: %v", err)
	}
	return buffer.Bytes()
}

type archiveServer struct {
	server    *httptest.Server
	downloads atomic.Int32
}

func serveArchive(t *testing.T, payload []byte, status int) *archiveServer {
	t.Helper()

	served := &archiveServer{}
	served.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.downloads.Add(1)
		w.WriteHeader(status)
		w.Write(payload)
	}))
	t.Cleanup(served.server.Close)
	return served
}

func denoArchive(t *testing.T, version string) (*mirror.Candidate, *archiveServer) {
	t.Helper()

	payload := buildZip(t, map[string]string{"deno": script("deno " + version + " (stable, release)")})
	served := serveArchive(t, payload, http.StatusOK)
	return &mirror.Candidate{
		Version: version,
		URL:     served.server.URL + "/deno.zip",
		SHA256:  sha256Hex(payload),
		Archive: mirror.ZipArchive,
		Members: []string{"deno"},
	}, served
}
