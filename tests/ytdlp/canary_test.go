package ytdlp_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/ytdlp"
)

func TestCanaryRingIsCapped(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	for i := 0; i < ytdlp.HookCanaryRingSize*2; i++ {
		versionmanager.HookAddToCanaryRing(string(rune('a' + i)))
	}

	if got := len(versionmanager.HookState().CanaryRing); got != ytdlp.HookCanaryRingSize {
		t.Errorf("got %d canary entries, want the ring capped at %d", got, ytdlp.HookCanaryRingSize)
	}

	newest := string(rune('a' + ytdlp.HookCanaryRingSize*2 - 1))
	if versionmanager.HookState().CanaryRing[ytdlp.HookCanaryRingSize-1] != newest {
		t.Errorf("got %q as the newest entry, want %q", versionmanager.HookState().CanaryRing[ytdlp.HookCanaryRingSize-1], newest)
	}
}

func TestGetCanaryIDsAlwaysIncludesFixedVideos(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	ids := versionmanager.HookGetCanaryIDs()
	if len(ids) != len(*ytdlp.HookFixedCanaryIDs) {
		t.Fatalf("got %d ids with an empty ring, want %d", len(ids), len(*ytdlp.HookFixedCanaryIDs))
	}

	for i := 0; i < ytdlp.HookCanaryRingSize; i++ {
		versionmanager.HookAddToCanaryRing(string(rune('a' + i)))
	}

	ids = versionmanager.HookGetCanaryIDs()
	if len(ids) != len(*ytdlp.HookFixedCanaryIDs)+ytdlp.HookCanaryTestCount {
		t.Errorf("got %d ids, want %d", len(ids), len(*ytdlp.HookFixedCanaryIDs)+ytdlp.HookCanaryTestCount)
	}
	present := map[string]bool{}
	for _, id := range ids {
		present[id] = true
	}
	for _, fixed := range *ytdlp.HookFixedCanaryIDs {
		if !present[fixed] {
			t.Errorf("fixed id %q is missing from %v", fixed, ids)
		}
	}
}

func TestRunCanaryPassesOnFirstSuccessfulExtraction(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `printf '{"id":"jNQXAC9IVRw","formats":[{"url":"`+server.URL+`"}]}'`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if !passed {
		t.Error("got passed=false, want a passing canary for a working binary")
	}
	if networkError {
		t.Error("got networkError=true, want false")
	}
}

func TestRunCanaryFailsOnBrokenExtractor(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `echo "ERROR: unable to extract player response" >&2; exit 1`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if passed {
		t.Error("got passed=true, want a failing canary for a broken extractor")
	}
	if networkError {
		t.Error("got networkError=true, want the failure attributed to the binary")
	}
}

func TestRunCanaryReportsNetworkFailuresSeparately(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `echo "ERROR: unable to download webpage: connection refused" >&2; exit 1`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if passed {
		t.Error("got passed=true, want false while the network is unreachable")
	}
	if !networkError {
		t.Error("got networkError=false, want network failures reported so the version stays pending")
	}
}

func TestRunCanaryTreatsUnavailableVideosAsInconclusive(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `echo "ERROR: Private video" >&2; exit 1`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if !passed {
		t.Error("got passed=false, want an unavailable video not to condemn the binary")
	}
	if networkError {
		t.Error("got networkError=true, want false")
	}
}

func TestCanaryRequestsAWellFormedWatchURL(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().CanaryRing = []string{"https://www.youtube.com/watch?v=ezXluhqaqfI", "tXHXkDqn_Ic"}

	argsFile := filepath.Join(t.TempDir(), "args")
	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `echo "$@" >> `+argsFile+`; exit 1`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	versionmanager.RunCanary("2026.07.04")

	recorded, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the fake binary recorded no arguments: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		if count := strings.Count(line, "watch?v="); count != 1 {
			t.Errorf("got %d occurrences of watch?v= in %q, want exactly 1: a full URL was pasted into the URL template", count, line)
		}
	}
}

func TestCanaryTreatsAnUnusableTargetAsInconclusive(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().CanaryRing = []string{"https://youtu.be/xjbleyEIFyA"}

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `echo "ERROR: Unsupported URL: $*" >&2; exit 1`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	result := versionmanager.HookTestExtraction(path, "https://youtu.be/xjbleyEIFyA")
	if !*result.HookInconclusive() {
		t.Errorf("got %+v, want inconclusive: a target that is not a video ID says nothing about the binary", result)
	}
}

func TestRunCanaryClearsABinaryWhenOnlyOneVideoIsBroken(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().CanaryRing = []string{"ezXluhqaqfI"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	script := `case "$*" in
*ezXluhqaqfI*) echo "ERROR: unable to extract player response" >&2; exit 1 ;;
*) printf '{"id":"ok","formats":[{"url":"` + server.URL + `"}]}' ;;
esac`
	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), script)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if !passed {
		t.Error("got passed=false, want the fixed videos to clear a binary that only one played video breaks")
	}
	if networkError {
		t.Error("got networkError=true, want false")
	}
}

func TestRunCanaryCondemnsABinaryTwoPlayedVideosBreak(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().CanaryRing = []string{"ezXluhqaqfI", "tXHXkDqn_Ic"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	argsFile := filepath.Join(t.TempDir(), "args")
	script := `echo "$@" >> ` + argsFile + `
case "$*" in
*ezXluhqaqfI*|*tXHXkDqn_Ic*) echo "ERROR: unable to extract player response" >&2; exit 1 ;;
*) printf '{"id":"ok","formats":[{"url":"` + server.URL + `"}]}' ;;
esac`
	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), script)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if passed {
		t.Error("got passed=true, want two failing played videos to condemn the binary even though the fixed videos still work")
	}
	if networkError {
		t.Error("got networkError=true, want the failure attributed to the binary")
	}

	recorded, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the fake binary recorded no arguments: %v", err)
	}
	for _, fixed := range *ytdlp.HookFixedCanaryIDs {
		if strings.Contains(string(recorded), fixed) {
			t.Errorf("fixed video %q was probed; the canary should stop once the quorum of played videos has failed", fixed)
		}
	}
}

func TestRunCanaryPassesWhenAPlayedVideoStillWorks(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().CanaryRing = []string{"ezXluhqaqfI", "tXHXkDqn_Ic"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	script := `case "$*" in
*ezXluhqaqfI*) echo "ERROR: unable to extract player response" >&2; exit 1 ;;
*) printf '{"id":"ok","formats":[{"url":"` + server.URL + `"}]}' ;;
esac`
	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), script)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if !passed {
		t.Error("got passed=false, want one broken played video not to condemn a binary that still extracts")
	}
	if networkError {
		t.Error("got networkError=true, want false")
	}
}

func TestRunCanaryStillFailsWhenTheFixedVideosBreakToo(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().CanaryRing = []string{"ezXluhqaqfI"}

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `echo "ERROR: unable to extract player response" >&2; exit 1`)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: path, State: ytdlp.StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if passed {
		t.Error("got passed=true, want a binary that breaks every video to still be condemned")
	}
	if networkError {
		t.Error("got networkError=true, want the failure attributed to the binary")
	}
}

func TestRunCanaryRejectsUnknownVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	if passed, networkError := versionmanager.RunCanary("2026.07.04"); passed || networkError {
		t.Errorf("got (%v, %v), want (false, false) for an untracked version", passed, networkError)
	}
}
