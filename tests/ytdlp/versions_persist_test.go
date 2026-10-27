package ytdlp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"noraegaori/internal/ytdlp"
)

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
