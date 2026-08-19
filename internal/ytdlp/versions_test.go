package ytdlp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newTestVersionManager(t *testing.T) *VersionManager {
	t.Helper()

	t.Chdir(t.TempDir())

	return &VersionManager{
		state: persistedState{
			Versions:   make(map[string]*VersionEntry),
			CanaryRing: []string{},
		},
	}
}

func addVersion(versionmanager *VersionManager, version string, entry *VersionEntry) {
	if entry.RegisteredAt.IsZero() {
		entry.RegisteredAt = time.Now()
	}
	versionmanager.state.Versions[version] = entry
}

func writeFakeBinary(t *testing.T, dir, script string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake executables are not portable to windows")
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create binary directory: %v", err)
	}

	path := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatalf("failed to write the fake binary: %v", err)
	}

	return path
}

func TestRegisterVersionIgnoresDuplicates(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	versionmanager.RegisterVersion("2026.07.04", "lib/yt-dlp-2026.07.04/yt-dlp")
	versionmanager.RegisterVersion("2026.07.04", "lib/other/yt-dlp")

	entry := versionmanager.state.Versions["2026.07.04"]
	if entry == nil {
		t.Fatal("the version was not registered")
	}
	if entry.Path != "lib/yt-dlp-2026.07.04/yt-dlp" {
		t.Errorf("got path %q, want the path from the first registration", entry.Path)
	}
	if entry.State != StatePending {
		t.Errorf("got state %q, want %q", entry.State, StatePending)
	}
}

func TestSetVersionStateRecordsBlacklistTime(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	versionmanager.SetVersionState("2026.07.04", StateBlacklisted)

	entry := versionmanager.state.Versions["2026.07.04"]
	if entry.State != StateBlacklisted {
		t.Errorf("got state %q, want %q", entry.State, StateBlacklisted)
	}
	if entry.BlacklistedAt.IsZero() {
		t.Error("BlacklistedAt was not stamped")
	}

	versionmanager.SetVersionState("does-not-exist", StateBlacklisted)
	if _, ok := versionmanager.state.Versions["does-not-exist"]; ok {
		t.Error("SetVersionState created an entry for an unknown version")
	}
}

func TestGetVersionStateReportsUnknownVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	if state, ok := versionmanager.GetVersionState("2026.07.04"); !ok || state != StatePending {
		t.Errorf("got (%q, %v), want (%q, true)", state, ok, StatePending)
	}
	if _, ok := versionmanager.GetVersionState("2026.01.01"); ok {
		t.Error("an unknown version was reported as known")
	}
}

func TestSetActiveVersionDemotesThePreviousActive(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})
	addVersion(versionmanager, "2026.07.05", &VersionEntry{Path: "lib/b/yt-dlp", State: StateVerified})
	versionmanager.state.ActiveVersion = "2026.07.04"

	versionmanager.SetActiveVersion("2026.07.05")

	if got := versionmanager.GetActiveVersion(); got != "2026.07.05" {
		t.Errorf("got active version %q, want %q", got, "2026.07.05")
	}
	if state := versionmanager.state.Versions["2026.07.04"].State; state != StateVerified {
		t.Errorf("the previous active is %q, want %q", state, StateVerified)
	}
	if state := versionmanager.state.Versions["2026.07.05"].State; state != StateActive {
		t.Errorf("the new active is %q, want %q", state, StateActive)
	}
}

func TestProvisionallyActivateClearsBlacklist(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{
		Path:          "lib/a/yt-dlp",
		State:         StateBlacklisted,
		BlacklistedAt: time.Now(),
	})

	versionmanager.ProvisionallyActivate("2026.07.04", "lib/a/yt-dlp")

	entry := versionmanager.state.Versions["2026.07.04"]
	if entry.State != StateProvisional {
		t.Errorf("got state %q, want %q", entry.State, StateProvisional)
	}
	if !entry.BlacklistedAt.IsZero() {
		t.Error("BlacklistedAt was not cleared")
	}
	if versionmanager.GetActiveVersion() != "2026.07.04" {
		t.Error("the provisional version was not made active")
	}
}

func TestProvisionallyActivateRegistersUnknownVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	versionmanager.ProvisionallyActivate("2026.07.04", "lib/a/yt-dlp")

	entry := versionmanager.state.Versions["2026.07.04"]
	if entry == nil {
		t.Fatal("the version was not registered")
	}
	if entry.Path != "lib/a/yt-dlp" {
		t.Errorf("got path %q, want %q", entry.Path, "lib/a/yt-dlp")
	}
}

func TestSaveErrorIgnoresUnactionableFailures(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})

	versionmanager.SaveError("2026.07.04", "video1", "ERROR: Private video")
	versionmanager.SaveError("2026.07.04", "video2", "dial tcp: connection refused")

	if got := len(versionmanager.state.Versions["2026.07.04"].Errors); got != 0 {
		t.Errorf("got %d saved errors, want 0 for unavailable and network failures", got)
	}
}

func TestSaveErrorDeduplicatesByVideo(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})

	versionmanager.SaveError("2026.07.04", "video1", "extractor broke")
	versionmanager.SaveError("2026.07.04", "video1", "extractor broke again")
	versionmanager.SaveError("2026.07.04", "video2", "extractor broke")

	if got := len(versionmanager.state.Versions["2026.07.04"].Errors); got != 2 {
		t.Errorf("got %d saved errors, want 2 distinct videos", got)
	}
}

func TestSaveErrorPrunesOutsideTheRollbackWindow(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{
		Path:  "lib/a/yt-dlp",
		State: StateActive,
		Errors: []ErrorRecord{
			{VideoID: "stale", Time: time.Now().Add(-2 * rollbackWindow)},
		},
	})

	versionmanager.SaveError("2026.07.04", "fresh", "extractor broke")

	errors := versionmanager.state.Versions["2026.07.04"].Errors
	if len(errors) != 1 {
		t.Fatalf("got %d errors, want only the fresh one", len(errors))
	}
	if errors[0].VideoID != "fresh" {
		t.Errorf("got %q, want the stale record pruned", errors[0].VideoID)
	}
}

func TestShouldRollbackHonoursThresholdAndWindow(t *testing.T) {
	recent := func(n int, age time.Duration) []ErrorRecord {
		records := make([]ErrorRecord, 0, n)
		for i := 0; i < n; i++ {
			records = append(records, ErrorRecord{VideoID: string(rune('a' + i)), Time: time.Now().Add(-age)})
		}
		return records
	}

	cases := []struct {
		name   string
		errors []ErrorRecord
		want   bool
	}{
		{"below threshold", recent(rollbackThreshold-1, time.Minute), false},
		{"at threshold", recent(rollbackThreshold, time.Minute), true},
		{"outside the window", recent(rollbackThreshold, 2*rollbackWindow), false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			versionmanager := newTestVersionManager(t)
			addVersion(versionmanager, "2026.07.04", &VersionEntry{
				Path:   "lib/a/yt-dlp",
				State:  StateActive,
				Errors: testCase.errors,
			})
			versionmanager.state.ActiveVersion = "2026.07.04"

			if got := versionmanager.shouldRollback(); got != testCase.want {
				t.Errorf("got %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestShouldRollbackIgnoresUnknownActiveVersion(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.state.ActiveVersion = "2026.07.04"

	if versionmanager.shouldRollback() {
		t.Error("got true, want false when the active version is not tracked")
	}
}

func TestSelectBestVersionSkipsUnusableCandidates(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.01", &VersionEntry{State: StateVerified, Successes: 5})
	addVersion(versionmanager, "2026.07.02", &VersionEntry{State: StateBlacklisted, Successes: 9})
	addVersion(versionmanager, "2026.07.03", &VersionEntry{State: StateVerified, Successes: 0})
	addVersion(versionmanager, "2026.07.04", &VersionEntry{State: StateActive, Successes: 9})
	versionmanager.state.ActiveVersion = "2026.07.04"

	if got := versionmanager.selectBestVersion(); got != "2026.07.01" {
		t.Errorf("got %q, want the newest non-blacklisted version with successes", got)
	}
}

func TestSelectBestVersionFallsBackToActive(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{State: StateActive, Successes: 3})
	versionmanager.state.ActiveVersion = "2026.07.04"

	if got := versionmanager.selectBestVersion(); got != "2026.07.04" {
		t.Errorf("got %q, want the active version when there is no alternative", got)
	}
}

func TestTryPromoteVerifiedPromotesNewerVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})
	addVersion(versionmanager, "2026.07.05", &VersionEntry{Path: "lib/b/yt-dlp", State: StateVerified})
	versionmanager.state.ActiveVersion = "2026.07.04"

	versionmanager.tryPromoteVerified()

	if versionmanager.state.ActiveVersion != "2026.07.05" {
		t.Errorf("got active %q, want %q", versionmanager.state.ActiveVersion, "2026.07.05")
	}
	if state := versionmanager.state.Versions["2026.07.04"].State; state != StateVerified {
		t.Errorf("the previous active is %q, want %q", state, StateVerified)
	}
}

func TestTryPromoteVerifiedIgnoresOlderVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})
	addVersion(versionmanager, "2026.07.01", &VersionEntry{Path: "lib/b/yt-dlp", State: StateVerified})
	versionmanager.state.ActiveVersion = "2026.07.04"

	versionmanager.tryPromoteVerified()

	if versionmanager.state.ActiveVersion != "2026.07.04" {
		t.Errorf("got active %q, want the newer version to stay active", versionmanager.state.ActiveVersion)
	}
}

func TestSaveSuccessPromotesProvisionalAfterStableRun(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateProvisional})
	versionmanager.state.ActiveVersion = "2026.07.04"

	for i := 0; i < stableSuccessCount-1; i++ {
		versionmanager.SaveSuccess("2026.07.04", "")
	}
	if state := versionmanager.state.Versions["2026.07.04"].State; state != StateProvisional {
		t.Fatalf("got state %q before the threshold, want %q", state, StateProvisional)
	}

	versionmanager.SaveSuccess("2026.07.04", "")

	if state := versionmanager.state.Versions["2026.07.04"].State; state != StateActive {
		t.Errorf("got state %q at the threshold, want %q", state, StateActive)
	}
}

func TestSaveSuccessRecordsCanaryVideos(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive})

	versionmanager.SaveSuccess("2026.07.04", "video1")
	versionmanager.SaveSuccess("2026.07.04", "video1")

	if got := len(versionmanager.state.CanaryRing); got != 1 {
		t.Errorf("got %d canary entries, want 1 after a duplicate", got)
	}
	if versionmanager.state.Versions["2026.07.04"].Successes != 2 {
		t.Errorf("got %d successes, want 2", versionmanager.state.Versions["2026.07.04"].Successes)
	}
}

func TestCanaryRingIsCapped(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	for i := 0; i < canaryRingSize*2; i++ {
		versionmanager.addToCanaryRing(string(rune('a' + i)))
	}

	if got := len(versionmanager.state.CanaryRing); got != canaryRingSize {
		t.Errorf("got %d canary entries, want the ring capped at %d", got, canaryRingSize)
	}

	newest := string(rune('a' + canaryRingSize*2 - 1))
	if versionmanager.state.CanaryRing[canaryRingSize-1] != newest {
		t.Errorf("got %q as the newest entry, want %q", versionmanager.state.CanaryRing[canaryRingSize-1], newest)
	}
}

func TestGetCanaryIDsAlwaysIncludesFixedVideos(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	ids := versionmanager.getCanaryIDs()
	if len(ids) != len(fixedCanaryIDs) {
		t.Fatalf("got %d ids with an empty ring, want %d", len(ids), len(fixedCanaryIDs))
	}

	for i := 0; i < canaryRingSize; i++ {
		versionmanager.addToCanaryRing(string(rune('a' + i)))
	}

	ids = versionmanager.getCanaryIDs()
	if len(ids) != len(fixedCanaryIDs)+canaryTestCount {
		t.Errorf("got %d ids, want %d", len(ids), len(fixedCanaryIDs)+canaryTestCount)
	}
	present := map[string]bool{}
	for _, id := range ids {
		present[id] = true
	}
	for _, fixed := range fixedCanaryIDs {
		if !present[fixed] {
			t.Errorf("fixed id %q is missing from %v", fixed, ids)
		}
	}
}

func TestCleanupOldVersionsKeepsActiveAndFallback(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	paths := map[string]string{}
	for _, version := range []string{"2026.07.01", "2026.07.02", "2026.07.03", "2026.07.04"} {
		paths[version] = writeFakeBinary(t, filepath.Join("lib", "yt-dlp-"+version), "exit 0")
	}

	addVersion(versionmanager, "2026.07.01", &VersionEntry{Path: paths["2026.07.01"], State: StateVerified, Successes: 4})
	addVersion(versionmanager, "2026.07.02", &VersionEntry{Path: paths["2026.07.02"], State: StateBlacklisted, Successes: 2})
	addVersion(versionmanager, "2026.07.03", &VersionEntry{Path: paths["2026.07.03"], State: StateVerified, Successes: 0})
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: paths["2026.07.04"], State: StateActive, Successes: 10})
	versionmanager.state.ActiveVersion = "2026.07.04"

	versionmanager.cleanupOldVersions()

	if _, ok := versionmanager.state.Versions["2026.07.04"]; !ok {
		t.Error("the active version was removed")
	}
	if _, ok := versionmanager.state.Versions["2026.07.01"]; !ok {
		t.Error("the fallback version was removed")
	}
	if _, ok := versionmanager.state.Versions["2026.07.02"]; ok {
		t.Error("the blacklisted version was kept")
	}
	if _, ok := versionmanager.state.Versions["2026.07.03"]; ok {
		t.Error("the superseded verified version was kept")
	}

	if _, err := os.Stat(filepath.Dir(paths["2026.07.02"])); !os.IsNotExist(err) {
		t.Error("the blacklisted version directory was left on disk")
	}
	if _, err := os.Stat(filepath.Dir(paths["2026.07.04"])); err != nil {
		t.Error("the active version directory was deleted")
	}
}

func TestCleanupOldVersionsRemovesStalePending(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	stale := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.01.01"), "exit 0")
	fresh := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.03"), "exit 0")
	active := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")

	addVersion(versionmanager, "2026.01.01", &VersionEntry{
		Path:         stale,
		State:        StatePending,
		RegisteredAt: time.Now().Add(-2 * stalePendingTimeout),
	})
	addVersion(versionmanager, "2026.07.03", &VersionEntry{Path: fresh, State: StatePending})
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: active, State: StateActive, Successes: 10})
	versionmanager.state.ActiveVersion = "2026.07.04"

	versionmanager.cleanupOldVersions()

	if _, ok := versionmanager.state.Versions["2026.01.01"]; ok {
		t.Error("the stale pending version was kept")
	}
	if _, ok := versionmanager.state.Versions["2026.07.03"]; !ok {
		t.Error("a freshly pending version was removed")
	}
}

func TestActiveBinaryPathRollsBackAfterRepeatedErrors(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	good := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 0")
	bad := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")

	errors := make([]ErrorRecord, 0, rollbackThreshold)
	for i := 0; i < rollbackThreshold; i++ {
		errors = append(errors, ErrorRecord{VideoID: string(rune('a' + i)), Time: time.Now()})
	}

	addVersion(versionmanager, "2026.07.01", &VersionEntry{Path: good, State: StateVerified, Successes: 7})
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: bad, State: StateActive, Errors: errors})
	versionmanager.state.ActiveVersion = "2026.07.04"

	if got := versionmanager.ActiveBinaryPath(); got != good {
		t.Errorf("got binary %q, want the rolled-back binary %q", got, good)
	}
	if versionmanager.state.Versions["2026.07.04"].State != StateBlacklisted {
		t.Error("the failing version was not blacklisted")
	}
	if versionmanager.GetActiveVersion() != "2026.07.01" {
		t.Errorf("got active %q, want %q", versionmanager.GetActiveVersion(), "2026.07.01")
	}
}

func TestActiveBinaryPathFallsBackToLegacyWhenBinaryIsMissing(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/yt-dlp-2026.07.04/yt-dlp", State: StateActive})
	versionmanager.state.ActiveVersion = "2026.07.04"

	if got := versionmanager.ActiveBinaryPath(); got != GetLegacyBinaryPath() {
		t.Errorf("got %q, want the legacy path %q", got, GetLegacyBinaryPath())
	}
}

func TestHasUsableBinaryChecksCandidatesOnDisk(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	broken := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 1")
	working := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")

	addVersion(versionmanager, "2026.07.01", &VersionEntry{Path: broken, State: StateVerified})
	if versionmanager.HasUsableBinary() {
		t.Error("a binary that fails --version was reported as usable")
	}

	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: working, State: StateActive})
	if !versionmanager.HasUsableBinary() {
		t.Error("a working binary was not found")
	}
}

func TestHasUsableBinaryIgnoresBlacklistedAndMissing(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	blacklisted := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 0")
	addVersion(versionmanager, "2026.07.01", &VersionEntry{Path: blacklisted, State: StateBlacklisted})
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/absent/yt-dlp", State: StateActive})

	if versionmanager.HasUsableBinary() {
		t.Error("got true, want false when every candidate is blacklisted or missing")
	}
}

func TestRunCanaryPassesOnFirstSuccessfulExtraction(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()

	path := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), `printf '{"id":"jNQXAC9IVRw","formats":[{"url":"`+server.URL+`"}]}'`)
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: path, State: StatePending})

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
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: path, State: StatePending})

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
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: path, State: StatePending})

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
	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: path, State: StatePending})

	passed, networkError := versionmanager.RunCanary("2026.07.04")
	if !passed {
		t.Error("got passed=false, want an unavailable video not to condemn the binary")
	}
	if networkError {
		t.Error("got networkError=true, want false")
	}
}

func TestRunCanaryRejectsUnknownVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	if passed, networkError := versionmanager.RunCanary("2026.07.04"); passed || networkError {
		t.Errorf("got (%v, %v), want (false, false) for an untracked version", passed, networkError)
	}
}

func TestPersistAndLoadRoundTrip(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	addVersion(versionmanager, "2026.07.04", &VersionEntry{Path: "lib/a/yt-dlp", State: StateActive, Successes: 4})
	versionmanager.state.ActiveVersion = "2026.07.04"
	versionmanager.addToCanaryRing("video1")
	checkedAt := time.Now().Truncate(time.Second)
	versionmanager.SetLastGitHubCheck(checkedAt)

	reloaded := &VersionManager{}
	if err := reloaded.load(); err != nil {
		t.Fatalf("load returned %v, want nil", err)
	}

	if reloaded.GetActiveVersion() != "2026.07.04" {
		t.Errorf("got active %q, want %q", reloaded.GetActiveVersion(), "2026.07.04")
	}
	if entry := reloaded.state.Versions["2026.07.04"]; entry == nil || entry.Successes != 4 {
		t.Error("the version entry did not survive the round trip")
	}
	if len(reloaded.state.CanaryRing) != 1 {
		t.Errorf("got %d canary entries, want 1", len(reloaded.state.CanaryRing))
	}
	if !reloaded.GetLastGitHubCheck().Equal(checkedAt) {
		t.Errorf("got check time %v, want %v", reloaded.GetLastGitHubCheck(), checkedAt)
	}
}

func TestLoadNormalizesEmptyCollections(t *testing.T) {
	newTestVersionManager(t)

	if err := os.MkdirAll(filepath.Dir(versionDataFile), 0755); err != nil {
		t.Fatalf("failed to create the data directory: %v", err)
	}
	if err := os.WriteFile(versionDataFile, []byte(`{"active_version":"2026.07.04"}`), 0644); err != nil {
		t.Fatalf("failed to write the state file: %v", err)
	}

	versionmanager := &VersionManager{}
	if err := versionmanager.load(); err != nil {
		t.Fatalf("load returned %v, want nil", err)
	}

	if versionmanager.state.Versions == nil {
		t.Error("Versions was left nil, so registration would panic")
	}
	if versionmanager.state.CanaryRing == nil {
		t.Error("CanaryRing was left nil")
	}
}

func TestLoadRejectsCorruptState(t *testing.T) {
	newTestVersionManager(t)

	if err := os.MkdirAll(filepath.Dir(versionDataFile), 0755); err != nil {
		t.Fatalf("failed to create the data directory: %v", err)
	}
	if err := os.WriteFile(versionDataFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write the state file: %v", err)
	}

	versionmanager := &VersionManager{}
	if err := versionmanager.load(); err == nil {
		t.Error("load returned nil, want an error for corrupt state")
	}
}

func TestInitVersionManagerStartsFreshWithoutState(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { versionMgr = nil })

	if err := InitVersionManager(); err != nil {
		t.Fatalf("InitVersionManager returned %v, want nil", err)
	}

	versionmanager := GetVersionManager()
	if versionmanager == nil {
		t.Fatal("GetVersionManager returned nil after initialization")
	}
	if len(versionmanager.state.Versions) != 0 {
		t.Errorf("got %d tracked versions, want a fresh state", len(versionmanager.state.Versions))
	}
}

func TestPersistWritesReadableJSON(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	data, err := os.ReadFile(versionDataFile)
	if err != nil {
		t.Fatalf("the state file was not written: %v", err)
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("the state file is not valid JSON: %v", err)
	}
	if _, ok := state.Versions["2026.07.04"]; !ok {
		t.Error("the registered version is missing from the persisted state")
	}

	if _, err := os.Stat(versionDataFile + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary state file was left behind")
	}
}
