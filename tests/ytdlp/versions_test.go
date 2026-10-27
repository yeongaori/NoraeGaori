package ytdlp_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"noraegaori/internal/ytdlp"
)

func createVersionManager(t *testing.T) *ytdlp.VersionManager {
	t.Helper()

	t.Chdir(t.TempDir())

	return ytdlp.HookCreateVersionManager(&ytdlp.HookVersionManagerFields{
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

func TestRegisterVersionDuplicates(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestSetVersionStateBlacklistTime(t *testing.T) {
	versionmanager := createVersionManager(t)
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

func TestGetVersionStateUnknown(t *testing.T) {
	versionmanager := createVersionManager(t)
	versionmanager.RegisterVersion("2026.07.04", "lib/a/yt-dlp")

	if state, ok := versionmanager.GetVersionState("2026.07.04"); !ok || state != ytdlp.StatePending {
		t.Errorf("got (%q, %v), want (%q, true)", state, ok, ytdlp.StatePending)
	}
	if _, ok := versionmanager.GetVersionState("2026.01.01"); ok {
		t.Error("an unknown version was reported as known")
	}
}

func TestSetActiveVersionDemotes(t *testing.T) {
	versionmanager := createVersionManager(t)
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

func TestActivateProvisionalClears(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{
		Path:          "lib/a/yt-dlp",
		State:         ytdlp.StateBlacklisted,
		BlacklistedAt: time.Now(),
	})

	versionmanager.ActivateProvisional("2026.07.04", "lib/a/yt-dlp")

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

func TestActivateProvisionalRegisters(t *testing.T) {
	versionmanager := createVersionManager(t)

	versionmanager.ActivateProvisional("2026.07.04", "lib/a/yt-dlp")

	entry := versionmanager.HookState().Versions["2026.07.04"]
	if entry == nil {
		t.Fatal("the version was not registered")
	}
	if entry.Path != "lib/a/yt-dlp" {
		t.Errorf("got path %q, want %q", entry.Path, "lib/a/yt-dlp")
	}
}

func TestSaveErrorUnactionable(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})

	versionmanager.SaveError("2026.07.04", "video1", "ERROR: Private video")
	versionmanager.SaveError("2026.07.04", "video2", "dial tcp: connection refused")

	if got := len(versionmanager.HookState().Versions["2026.07.04"].Errors); got != 0 {
		t.Errorf("got %d saved errors, want 0 for unavailable and network failures", got)
	}
}

func TestSaveErrorIgnoresRateLimits(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	for _, videoID := range []string{"video1", "video2", "video3", "video4"} {
		versionmanager.SaveError("2026.07.04", videoID, "WARNING: [youtube] "+videoID+": Unable to download webpage: HTTP Error 429: Too Many Requests")
	}

	if got := len(versionmanager.HookState().Versions["2026.07.04"].Errors); got != 0 {
		t.Errorf("got %d saved errors, want YouTube rate limits not blamed on yt-dlp", got)
	}
	if versionmanager.IsActiveFailing() {
		t.Error("the active version is reported failing because of YouTube rate limits")
	}
}

func TestSaveErrorDeduplicatesByVideo(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})

	versionmanager.SaveError("2026.07.04", "video1", "extractor broke")
	versionmanager.SaveError("2026.07.04", "video1", "extractor broke again")
	versionmanager.SaveError("2026.07.04", "video2", "extractor broke")

	if got := len(versionmanager.HookState().Versions["2026.07.04"].Errors); got != 2 {
		t.Errorf("got %d saved errors, want 2 distinct videos", got)
	}
}

func TestSaveErrorPrunesWindow(t *testing.T) {
	versionmanager := createVersionManager(t)
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

func TestShouldRollbackThreshold(t *testing.T) {
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

	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			versionmanager := createVersionManager(t)
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

func TestShouldRollbackUnknown(t *testing.T) {
	versionmanager := createVersionManager(t)
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if versionmanager.HookShouldRollback() {
		t.Error("got true, want false when the active version is not tracked")
	}
}

func TestSelectBestVersionUnusable(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{State: ytdlp.StateVerified, Successes: 5})
	addVersion(versionmanager, "2026.07.02", &ytdlp.VersionEntry{State: ytdlp.StateBlacklisted, Successes: 9})
	addVersion(versionmanager, "2026.07.03", &ytdlp.VersionEntry{State: ytdlp.StateVerified, Successes: 0})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{State: ytdlp.StateActive, Successes: 9})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.HookSelectBestVersion(); got != "2026.07.01" {
		t.Errorf("got %q, want the newest non-blacklisted version with successes", got)
	}
}

func TestSelectBestVersionActive(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{State: ytdlp.StateActive, Successes: 3})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.HookSelectBestVersion(); got != "2026.07.04" {
		t.Errorf("got %q, want the active version when there is no alternative", got)
	}
}

func TestTryPromoteVerifiedNewer(t *testing.T) {
	versionmanager := createVersionManager(t)
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

func TestTryPromoteVerifiedOlder(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/a/yt-dlp", State: ytdlp.StateActive})
	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: "lib/b/yt-dlp", State: ytdlp.StateVerified})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	versionmanager.HookTryPromoteVerified()

	if versionmanager.HookState().ActiveVersion != "2026.07.04" {
		t.Errorf("got active %q, want the newer version to stay active", versionmanager.HookState().ActiveVersion)
	}
}

func TestSaveSuccessPromotes(t *testing.T) {
	versionmanager := createVersionManager(t)
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

func TestSaveSuccessCanaryVideos(t *testing.T) {
	versionmanager := createVersionManager(t)
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

func TestCleanupOldVersionsKeeps(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestCleanupOldVersionsStale(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestCleanupTakesLock(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestCleanupConcurrentResults(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestActiveBinaryPathRollsBack(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestActiveBinaryPathLegacy(t *testing.T) {
	versionmanager := createVersionManager(t)
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/yt-dlp-2026.07.04/yt-dlp", State: ytdlp.StateActive})
	versionmanager.HookState().ActiveVersion = "2026.07.04"

	if got := versionmanager.ActiveBinaryPath(); got != ytdlp.GetLegacyBinaryPath() {
		t.Errorf("got %q, want the legacy path %q", got, ytdlp.GetLegacyBinaryPath())
	}
}

func TestHasUsableBinaryOnDisk(t *testing.T) {
	versionmanager := createVersionManager(t)

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

func TestHasUsableBinaryIgnores(t *testing.T) {
	versionmanager := createVersionManager(t)

	blacklisted := writeFakeBinary(t, filepath.Join("lib", "yt-dlp-2026.07.01"), "exit 0")
	addVersion(versionmanager, "2026.07.01", &ytdlp.VersionEntry{Path: blacklisted, State: ytdlp.StateBlacklisted})
	addVersion(versionmanager, "2026.07.04", &ytdlp.VersionEntry{Path: "lib/absent/yt-dlp", State: ytdlp.StateActive})

	if versionmanager.HasUsableBinary() {
		t.Error("got true, want false when every candidate is blacklisted or missing")
	}
}
