package ytdlp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"noraegaori/internal/ytdlp"
)

func newTestVersionManager(t *testing.T) *ytdlp.VersionManager {
	t.Helper()

	t.Chdir(t.TempDir())

	return ytdlp.HookBuildVersionManager(ytdlp.HookVersionManagerFields{
		State: ytdlp.HookPersistedState{
			Versions:   make(map[string]*ytdlp.VersionEntry),
			CanaryRing: []string{},
		},
	})
}

func addVersion(versionmanager *ytdlp.VersionManager, version string, entry *ytdlp.VersionEntry) {
	if entry.RegisteredAt.IsZero() {
		entry.RegisteredAt = time.Now()
	}
	versionmanager.HookState().Versions[version] = entry
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

	entry := versionmanager.HookState().Versions["2026.07.04"]
	if entry == nil {
		t.Fatal("the version was not registered")
	}
	if entry.Path != "lib/yt-dlp-2026.07.04/yt-dlp" {
		t.Errorf("got path %q, want the path from the first registration", entry.Path)
	}
	if entry.State != ytdlp.StatePending {
		t.Errorf("got state %q, want %q", entry.State, ytdlp.StatePending)
	}
}

func TestSetVersionStateRecordsBlacklistTime(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	versionmanager.SetVersionState("2026.07.04", ytdlp.StateBlacklisted)

	entry := versionmanager.HookState().Versions["2026.07.04"]
	if entry.State != ytdlp.StateBlacklisted {
		t.Errorf("got state %q, want %q", entry.State, ytdlp.StateBlacklisted)
	}
	if entry.BlacklistedAt.IsZero() {
		t.Error("BlacklistedAt was not stamped")
	}

	versionmanager.SetVersionState("does-not-exist", ytdlp.StateBlacklisted)
	if _, ok := versionmanager.HookState().Versions["does-not-exist"]; ok {
		t.Error("SetVersionState created an entry for an unknown version")
	}
}

func TestGetVersionStateReportsUnknownVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	if state, ok := versionmanager.GetVersionState("2026.07.04"); !ok || state != ytdlp.StatePending {
		t.Errorf("got (%q, %v), want (%q, true)", state, ok, ytdlp.StatePending)
	}
	if _, ok := versionmanager.GetVersionState("2026.01.01"); ok {
		t.Error("an unknown version was reported as known")
	}
}

func TestSetActiveVersionDemotesThePreviousActive(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})
	addVersion(versionmanager, "2026.07.05", &ytdlp.VersionEntry{Path: "lib/b/yt-dlp", State: ytdlp.StateVerified})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.SetActiveVersion("2026.07.05")

	if got := versionmanager.GetActiveVersion(); got != "2026.07.05" {
		t.Errorf("got active version %q, want %q", got, "2026.07.05")
	}
	if state := versionmanager.HookState().Versions["2026.07.04"].State; state != ytdlp.StateVerified {
		t.Errorf("the previous active is %q, want %q", state, ytdlp.StateVerified)
	}
	if state := versionmanager.HookState().Versions["2026.07.05"].State; state != ytdlp.StateActive {
		t.Errorf("the new active is %q, want %q", state, ytdlp.StateActive)
	}
}

func TestProvisionallyActivateClearsBlacklist(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{
		Path:          "lib/a/yt-dlp",
		State:         ytdlp.StateBlacklisted,
		BlacklistedAt: time.Now(),
	})

	versionmanager.ProvisionallyActivate("2026.07.04", "lib/a/yt-dlp")

	entry := versionmanager.HookState().Versions["2026.07.04"]
	if entry.State != ytdlp.StateProvisional {
		t.Errorf("got state %q, want %q", entry.State, ytdlp.StateProvisional)
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

	entry := versionmanager.HookState().Versions["2026.07.04"]
	if entry == nil {
		t.Fatal("the version was not registered")
	}
	if entry.Path != "lib/a/yt-dlp" {
		t.Errorf("got path %q, want %q", entry.Path, "lib/a/yt-dlp")
	}
}

func TestSaveErrorIgnoresUnactionableFailures(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})

	versionmanager.SaveError("2026.07.04", "video1", "ERROR: Private video")
	versionmanager.SaveError("2026.07.04", "video2", "dial tcp: connection refused")

	if got := len(versionmanager.HookState().Versions["2026.07.04"].Errors); got != 0 {
		t.Errorf("got %d saved errors, want 0 for unavailable and network failures", got)
	}
}

func TestSaveErrorIgnoresRateLimits(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	for _, videoID := range []string{"video1", "video2", "video3", "video4"} {
		versionmanager.SaveError("2026.07.04", videoID, "WARNING: [youtube] "+videoID+": Unable to download webpage: HTTP Error 429: Too Many Requests")
	}

	if got := len(versionmanager.HookState().Versions["2026.07.04"].Errors); got != 0 {
		t.Errorf("got %d saved errors, want YouTube rate limits not blamed on yt-dlp", got)
	}
	if versionmanager.ActiveVersionIsFailing() {
		t.Error("the active version is reported failing because of YouTube rate limits")
	}
}

func TestSaveErrorDeduplicatesByVideo(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})

	versionmanager.SaveError("2026.07.04", "video1", "extractor broke")
	versionmanager.SaveError("2026.07.04", "video1", "extractor broke again")
	versionmanager.SaveError("2026.07.04", "video2", "extractor broke")

	if got := len(versionmanager.HookState().Versions["2026.07.04"].Errors); got != 2 {
		t.Errorf("got %d saved errors, want 2 distinct videos", got)
	}
}

func TestSaveErrorPrunesOutsideTheRollbackWindow(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{
		Path:  "lib/a/yt-dlp",
		State: ytdlp.StateActive,
		Errors: []ytdlp.ErrorRecord{
			{VideoID: "stale", Time: time.Now().Add(-2 * ytdlp.HookRollbackWindow)},
		},
	})

	versionmanager.SaveError("2026.07.04", "fresh", "extractor broke")

	errors := versionmanager.HookState().Versions["2026.07.04"].Errors
	if len(errors) != 1 {
		t.Fatalf("got %d errors, want only the fresh one", len(errors))
	}
	if errors[0].VideoID != "fresh" {
		t.Errorf("got %q, want the stale record pruned", errors[0].VideoID)
	}
}

func TestShouldRollbackHonoursThresholdAndWindow(t *testing.T) {
	recent := func(n int, age time.Duration) []ytdlp.ErrorRecord {
		records := make([]ytdlp.ErrorRecord, 0, n)
		for i := 0; i < n; i++ {
			records = append(records, ytdlp.ErrorRecord{VideoID: string(rune('a' + i)), Time: time.Now().Add(-age)})
		}
		return records
	}

	cases := []struct {
		name   string
		errors []ytdlp.ErrorRecord
		want   bool
	}{
		{"below threshold", recent(ytdlp.HookRollbackThreshold-1, time.Minute), false},
		{"at threshold", recent(ytdlp.HookRollbackThreshold, time.Minute), true},
		{"outside the window", recent(ytdlp.HookRollbackThreshold, 2*ytdlp.HookRollbackWindow), false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			versionmanager := newTestVersionManager(t)
			addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{
				Path:   "lib/a/yt-dlp",
				State:  ytdlp.StateActive,
				Errors: testCase.errors,
			})
			versionmanager.HookState().ActiveVersion = "2026.07.04"

			if got := versionmanager.HookShouldRollback(); got != testCase.want {
				t.Errorf("got %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestShouldRollbackIgnoresUnknownActiveVersion(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if versionmanager.HookShouldRollback() {
		t.Error("got true, want false when the active version is not tracked")
	}
}

func TestSelectBestVersionSkipsUnusableCandidates(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{State: ytdlp.StateVerified, Successes: 5})
	addVersion(versionmanager, "2026.07.02", &ytdlp.VersionEntry{State: ytdlp.StateBlacklisted, Successes: 9})
	addVersion(versionmanager, "2026.07.03", &ytdlp.VersionEntry{State: ytdlp.StateVerified, Successes: 0})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{State: ytdlp.StateActive, Successes: 9})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.HookSelectBestVersion(); got != "2026.07.01" {
		t.Errorf("got %q, want the newest non-blacklisted version with successes", got)
	}
}

func TestSelectBestVersionFallsBackToActive(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{State: ytdlp.StateActive, Successes: 3})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.HookSelectBestVersion(); got != "2026.07.04" {
		t.Errorf("got %q, want the active version when there is no alternative", got)
	}
}

func TestTryPromoteVerifiedPromotesNewerVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})
	addVersion(versionmanager, "2026.07.05", &ytdlp.VersionEntry{Path: "lib/b/yt-dlp", State: ytdlp.StateVerified})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.HookTryPromoteVerified()

	if versionmanager.HookState().ActiveVersion != "2026.07.05" {
		t.Errorf("got active %q, want %q", versionmanager.HookState().ActiveVersion, "2026.07.05")
	}
	if state := versionmanager.HookState().Versions["2026.07.04"].State; state != ytdlp.StateVerified {
		t.Errorf("the previous active is %q, want %q", state, ytdlp.StateVerified)
	}
}

func TestTryPromoteVerifiedIgnoresOlderVersions(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})
	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: "lib/b/yt-dlp", State: ytdlp.StateVerified})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.HookTryPromoteVerified()

	if versionmanager.HookState().ActiveVersion != "2026.07.04" {
		t.Errorf("got active %q, want the newer version to stay active", versionmanager.HookState().ActiveVersion)
	}
}

func TestSaveSuccessPromotesProvisionalAfterStableRun(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateProvisional})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	for i := 0; i < ytdlp.HookStableSuccessCount-1; i++ {
		versionmanager.SaveSuccess("2026.07.04", "")
	}
	if state := versionmanager.HookState().Versions["2026.07.04"].State; state != ytdlp.StateProvisional {
		t.Fatalf("got state %q before the threshold, want %q", state, ytdlp.StateProvisional)
	}

	versionmanager.SaveSuccess("2026.07.04", "")

	if state := versionmanager.HookState().Versions["2026.07.04"].State; state != ytdlp.StateActive {
		t.Errorf("got state %q at the threshold, want %q", state, ytdlp.StateActive)
	}
}

func TestSaveSuccessRecordsCanaryVideos(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})

	versionmanager.SaveSuccess("2026.07.04", "video1")
	versionmanager.SaveSuccess("2026.07.04", "video1")

	if got := len(versionmanager.HookState().CanaryRing); got != 1 {
		t.Errorf("got %d canary entries, want 1 after a duplicate", got)
	}
	if versionmanager.HookState().Versions["2026.07.04"].Successes != 2 {
		t.Errorf("got %d successes, want 2", versionmanager.HookState().Versions["2026.07.04"].Successes)
	}
}

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

func TestCleanupOldVersionsKeepsActiveAndFallback(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	paths := map[string]string{}
	for _, version := range []string{"2026.07.01", "2026.07.02", "2026.07.03", "2026.07.04"} {
		paths[version] = writeFakeBinary(t, filepath.Join("lib", "yt-dlp-"+version), "exit 0")
	}

	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: paths["2026.07.01"], State: ytdlp.StateVerified, Successes: 4})
	addVersion(versionmanager, "2026.07.02", &ytdlp.VersionEntry{Path: paths["2026.07.02"], State: ytdlp.StateBlacklisted, Successes: 2})
	addVersion(versionmanager, "2026.07.03", &ytdlp.VersionEntry{Path: paths["2026.07.03"], State: ytdlp.StateVerified, Successes: 0})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: paths["2026.07.04"], State: ytdlp.StateActive, Successes: 10})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.HookCleanupOldVersions()

	if _, ok := versionmanager.HookState().Versions["2026.07.04"]; !ok {
		t.Error("the active version was removed")
	}
	if _, ok := versionmanager.HookState().Versions["2026.07.01"]; !ok {
		t.Error("the fallback version was removed")
	}
	if _, ok := versionmanager.HookState().Versions["2026.07.02"]; ok {
		t.Error("the blacklisted version was kept")
	}
	if _, ok := versionmanager.HookState().Versions["2026.07.03"]; ok {
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

	addVersion(versionmanager, "2026.01.01", &ytdlp.VersionEntry{
		Path:         stale,
		State:        ytdlp.StatePending,
		RegisteredAt: time.Now().Add(-2 * ytdlp.HookStalePendingTimeout),
	})
	addVersion(versionmanager, "2026.07.03", &ytdlp.VersionEntry{Path: fresh, State: ytdlp.StatePending})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: active, State: ytdlp.StateActive, Successes: 10})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.HookCleanupOldVersions()

	if _, ok := versionmanager.HookState().Versions["2026.01.01"]; ok {
		t.Error("the stale pending version was kept")
	}
	if _, ok := versionmanager.HookState().Versions["2026.07.03"]; !ok {
		t.Error("a freshly pending version was removed")
	}
}

func TestCleanupTakesTheLockItself(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	stale := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.01.01"), "exit 0")
	active := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")

	addVersion(versionmanager, "2026.01.01", &ytdlp.VersionEntry{
		Path:         stale,
		State:        ytdlp.StatePending,
		RegisteredAt: time.Now().Add(-2 * ytdlp.HookStalePendingTimeout),
	})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: active, State: ytdlp.StateActive, Successes: 400})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.Cleanup()

	if _, ok := versionmanager.HookState().Versions["2026.01.01"]; ok {
		t.Error("the stale pending version survived a scheduled cleanup on a version far past its success trigger")
	}
}

func TestCleanupIsSafeAlongsidePlaybackResults(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	active := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: active, State: ytdlp.StateActive})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	var waiter sync.WaitGroup
	waiter.Add(2)

	go func() {
		defer waiter.Done()
		for i := 0; i < 50; i++ {
			versionmanager.Cleanup()
		}
	}()
	go func() {
		defer waiter.Done()
		for i := 0; i < 50; i++ {
			versionmanager.SaveSuccess("2026.07.04", "jNQXAC9IVRw")
		}
	}()

	waiter.Wait()
}

func TestActiveBinaryPathRollsBackAfterRepeatedErrors(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	good := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 0")
	bad := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")

	errors := make([]ytdlp.ErrorRecord, 0, ytdlp.HookRollbackThreshold)
	for i := 0; i < ytdlp.HookRollbackThreshold; i++ {
		errors = append(errors, ytdlp.ErrorRecord{VideoID: string(rune('a' + i)), Time: time.Now()})
	}

	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: good, State: ytdlp.StateVerified, Successes: 7})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: bad, State: ytdlp.StateActive, Errors: errors})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.ActiveBinaryPath(); got != good {
		t.Errorf("got binary %q, want the rolled-back binary %q", got, good)
	}
	if versionmanager.HookState().Versions["2026.07.04"].State != ytdlp.StateBlacklisted {
		t.Error("the failing version was not blacklisted")
	}
	if versionmanager.GetActiveVersion() != "2026.07.01" {
		t.Errorf("got active %q, want %q", versionmanager.GetActiveVersion(), "2026.07.01")
	}
}

func TestActiveBinaryPathFallsBackToLegacyWhenBinaryIsMissing(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/yt-dlp-2026.07.04/yt-dlp", State: ytdlp.StateActive})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.ActiveBinaryPath(); got != ytdlp.GetLegacyBinaryPath() {
		t.Errorf("got %q, want the legacy path %q", got, ytdlp.GetLegacyBinaryPath())
	}
}

func TestHasUsableBinaryChecksCandidatesOnDisk(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	broken := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 1")
	working := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.04"), "exit 0")

	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: broken, State: ytdlp.StateVerified})
	if versionmanager.HasUsableBinary() {
		t.Error("a binary that fails --version was reported as usable")
	}

	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: working, State: ytdlp.StateActive})
	if !versionmanager.HasUsableBinary() {
		t.Error("a working binary was not found")
	}
}

func TestHasUsableBinaryIgnoresBlacklistedAndMissing(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	blacklisted := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 0")
	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: blacklisted, State: ytdlp.StateBlacklisted})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/absent/yt-dlp", State: ytdlp.StateActive})

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

func TestPersistAndLoadRoundTrip(t *testing.T) {
	versionmanager := newTestVersionManager(t)

	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive, Successes: 4})
	versionmanager.HookState().ActiveVersion = "2026.07.04"
	versionmanager.HookAddToCanaryRing("video1")
	checkedAt := time.Now().Truncate(time.Second)
	versionmanager.SetLastGitHubCheck(checkedAt)

	reloaded := &ytdlp.VersionManager{}
	if err := reloaded.HookLoad(); err != nil {
		t.Fatalf("load returned %v, want nil", err)
	}

	if reloaded.GetActiveVersion() != "2026.07.04" {
		t.Errorf("got active %q, want %q", reloaded.GetActiveVersion(), "2026.07.04")
	}
	if entry := reloaded.HookState().Versions["2026.07.04"]; entry == nil || entry.Successes != 4 {
		t.Error("the version entry did not survive the round trip")
	}
	if len(reloaded.HookState().CanaryRing) != 1 {
		t.Errorf("got %d canary entries, want 1", len(reloaded.HookState().CanaryRing))
	}
	if !reloaded.GetLastGitHubCheck().Equal(checkedAt) {
		t.Errorf("got check time %v, want %v", reloaded.GetLastGitHubCheck(), checkedAt)
	}
}

func TestLoadNormalizesEmptyCollections(t *testing.T) {
	newTestVersionManager(t)

	if err := os.MkdirAll(filepath.Dir(ytdlp.HookVersionDataFile), 0755); err != nil {
		t.Fatalf("failed to create the data directory: %v", err)
	}
	if err := os.WriteFile(ytdlp.HookVersionDataFile, []byte(`{"active_version":"2026.07.04"}`), 0644); err != nil {
		t.Fatalf("failed to write the state file: %v", err)
	}

	versionmanager := &ytdlp.VersionManager{}
	if err := versionmanager.HookLoad(); err != nil {
		t.Fatalf("load returned %v, want nil", err)
	}

	if versionmanager.HookState().Versions == nil {
		t.Error("Versions was left nil, so registration would panic")
	}
	if versionmanager.HookState().CanaryRing == nil {
		t.Error("CanaryRing was left nil")
	}
}

func TestLoadRejectsCorruptState(t *testing.T) {
	newTestVersionManager(t)

	if err := os.MkdirAll(filepath.Dir(ytdlp.HookVersionDataFile), 0755); err != nil {
		t.Fatalf("failed to create the data directory: %v", err)
	}
	if err := os.WriteFile(ytdlp.HookVersionDataFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write the state file: %v", err)
	}

	versionmanager := &ytdlp.VersionManager{}
	if err := versionmanager.HookLoad(); err == nil {
		t.Error("load returned nil, want an error for corrupt state")
	}
}

func TestInitVersionManagerStartsFreshWithoutState(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { *ytdlp.HookVersionMgr = nil })

	if err := ytdlp.InitVersionManager(); err != nil {
		t.Fatalf("InitVersionManager returned %v, want nil", err)
	}

	versionmanager := ytdlp.GetVersionManager()
	if versionmanager == nil {
		t.Fatal("GetVersionManager returned nil after initialization")
	}
	if len(versionmanager.HookState().Versions) != 0 {
		t.Errorf("got %d tracked versions, want a fresh state", len(versionmanager.HookState().Versions))
	}
}

func TestPersistWritesReadableJSON(t *testing.T) {
	versionmanager := newTestVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	data, err := os.ReadFile(ytdlp.HookVersionDataFile)
	if err != nil {
		t.Fatalf("the state file was not written: %v", err)
	}

	var state ytdlp.HookPersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("the state file is not valid JSON: %v", err)
	}
	if _, ok := state.Versions["2026.07.04"]; !ok {
		t.Error("the registered version is missing from the persisted state")
	}

	if _, err := os.Stat(ytdlp.HookVersionDataFile + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary state file was left behind")
	}
}
