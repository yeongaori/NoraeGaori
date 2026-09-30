package download_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/download"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/configtest"
)

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func servePayload(t *testing.T, status int, payload []byte) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(status)
		w.Write(payload)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestSaveFileReturnsTheContentDigest(t *testing.T) {
	payload := []byte("yt-dlp binary contents")
	destination := filepath.Join(t.TempDir(), "downloaded")

	sum, err := download.SaveFile(servePayload(t, http.StatusOK, payload), destination)
	if err != nil {
		t.Fatalf("SaveFile returned %v, want nil", err)
	}
	if sum != sha256Hex(payload) {
		t.Errorf("got digest %q, want %q", sum, sha256Hex(payload))
	}

	written, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("failed to read the downloaded file: %v", err)
	}
	if string(written) != string(payload) {
		t.Errorf("got file contents %q, want %q", written, payload)
	}
}

func TestSaveFileRejectsAFailedResponse(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "downloaded")

	if _, err := download.SaveFile(servePayload(t, http.StatusInternalServerError, nil), destination); err == nil {
		t.Error("SaveFile returned nil, want an error for a 500 response")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Error("a failed download left a file behind")
	}
}

func TestSaveFileRejectsAnUnwritableDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "missing", "downloaded")

	if _, err := download.SaveFile(servePayload(t, http.StatusOK, []byte("payload")), destination); err == nil {
		t.Error("SaveFile returned nil, want an error when the destination directory is missing")
	}
}

func serveInChunks(t *testing.T, chunks int, gap time.Duration, hangAfter bool) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for range chunks {
			w.Write([]byte(strings.Repeat("x", 1024)))
			flusher.Flush()
			time.Sleep(gap)
		}
		if hangAfter {
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func stubDownloadProgress(t *testing.T) (*[]int, *int) {
	t.Helper()

	percentages := []int{}
	ended := 0
	testutil.Swap(t, download.HookShowProgress, func(percent int, message string) {
		if !strings.HasSuffix(message, fmt.Sprintf(": %d%%", percent)) {
			t.Errorf("got message %q for %d%%, want the percentage at the end", message, percent)
		}
		percentages = append(percentages, percent)
	})
	testutil.Swap(t, download.HookEndProgress, func() { ended++ })
	return &percentages, &ended
}

func TestSaveFileReportsProgressUpToOneHundred(t *testing.T) {
	percentages, ended := stubDownloadProgress(t)
	payload := []byte(strings.Repeat("x", 100*1024))

	if _, err := download.SaveFile(servePayload(t, http.StatusOK, payload), filepath.Join(t.TempDir(), "downloaded")); err != nil {
		t.Fatalf("SaveFile returned %v, want nil", err)
	}

	if len(*percentages) == 0 || (*percentages)[len(*percentages)-1] != 100 {
		t.Fatalf("got %v, want the progress to end at 100", *percentages)
	}
	for i := 1; i < len(*percentages); i++ {
		if (*percentages)[i] <= (*percentages)[i-1] {
			t.Fatalf("got %v, want strictly increasing percentages", *percentages)
		}
	}
	if *ended != 1 {
		t.Errorf("ended the progress line %d times, want once", *ended)
	}
}

func TestSaveFileEndsTheProgressLineWhenItStalls(t *testing.T) {
	testutil.Swap(t, download.HookDownloadStallTimeout, 150*time.Millisecond)
	_, ended := stubDownloadProgress(t)

	if _, err := download.SaveFile(serveInChunks(t, 1, 0, true), filepath.Join(t.TempDir(), "downloaded")); err == nil {
		t.Fatal("SaveFile returned nil, want the stall reported")
	}
	if *ended != 1 {
		t.Errorf("ended the progress line %d times, want once after a stall", *ended)
	}
}

func TestSaveFileReportsNoPercentagesForAnUnknownSize(t *testing.T) {
	percentages, _ := stubDownloadProgress(t)

	if _, err := download.SaveFile(serveInChunks(t, 4, 0, false), filepath.Join(t.TempDir(), "downloaded")); err != nil {
		t.Fatalf("SaveFile returned %v, want nil", err)
	}
	if len(*percentages) != 0 {
		t.Errorf("got %v, want no percentages without a Content-Length", *percentages)
	}
}

func TestDownloadsWaitTwoMinutesForAStalledMirror(t *testing.T) {
	if *download.HookDownloadStallTimeout != 2*time.Minute {
		t.Errorf("got a stall window of %v, want 2m so a slow mirror is not abandoned after a short pause", *download.HookDownloadStallTimeout)
	}
}

func TestSaveFileGivesUpOnAStalledDownload(t *testing.T) {
	testutil.Swap(t, download.HookDownloadStallTimeout, 150*time.Millisecond)
	destination := filepath.Join(t.TempDir(), "downloaded")

	started := time.Now()
	_, err := download.SaveFile(serveInChunks(t, 1, 0, true), destination)
	if err == nil || !strings.Contains(err.Error(), "the mirror sent nothing for 150ms, so the download was abandoned") {
		t.Fatalf("got %v, want the stalled download reported", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("took %v, want the stall noticed shortly after data stopped", elapsed)
	}
}

func TestSaveFileKeepsASlowButSteadyDownload(t *testing.T) {
	testutil.Swap(t, download.HookDownloadStallTimeout, 150*time.Millisecond)
	destination := filepath.Join(t.TempDir(), "downloaded")

	sum, err := download.SaveFile(serveInChunks(t, 8, 60*time.Millisecond, false), destination)
	if err != nil {
		t.Fatalf("got %v, want a download that keeps receiving data to finish", err)
	}
	if want := sha256Hex([]byte(strings.Repeat("x", 8*1024))); sum != want {
		t.Errorf("got digest %q, want %q", sum, want)
	}
}

func TestSaveFileGivesUpWhenNoResponseArrives(t *testing.T) {
	testutil.Swap(t, download.HookDownloadStallTimeout, 150*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	if _, err := download.SaveFile(server.URL, filepath.Join(t.TempDir(), "downloaded")); err == nil {
		t.Error("got nil, want an error when the server never answers")
	}
}

func TestRateLimitDefaultsToTenMbps(t *testing.T) {
	configtest.Setup(t)

	if got := download.HookRateLimitBytesPerSecond(); got != 1250000 {
		t.Errorf("got %v bytes/sec, want 1250000 for the default 10 Mbps", got)
	}
}

func TestRateLimitFollowsTheConfiguredSpeed(t *testing.T) {
	configtest.SetupWith(t, `{"prefix":"!","language":"en","max_download_speed_mbps":24}`)

	if got := download.HookRateLimitBytesPerSecond(); got != 3000000 {
		t.Errorf("got %v bytes/sec, want 3000000 for 24 Mbps", got)
	}
}
