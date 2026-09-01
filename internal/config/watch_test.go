package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeWatchedFile(t *testing.T, path, contents string) os.FileInfo {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	return info
}

func TestIsDuplicateWriteEventTracksModTimePerPath(t *testing.T) {
	t.Chdir(t.TempDir())

	path := filepath.Join(t.TempDir(), "config.json")
	info := writeWatchedFile(t, path, "{}")

	if isDuplicateWriteEvent(path, info) {
		t.Error("the first event for a path was reported as a duplicate")
	}
	if !isDuplicateWriteEvent(path, info) {
		t.Error("a repeat event with the same ModTime was not reported as a duplicate")
	}

	other := filepath.Join(t.TempDir(), "admins.json")
	otherInfo := writeWatchedFile(t, other, "{}")
	if isDuplicateWriteEvent(other, otherInfo) {
		t.Error("a different path was reported as a duplicate")
	}
}

func TestIsDuplicateWriteEventAcceptsAChangedModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	info := writeWatchedFile(t, path, "{}")

	if isDuplicateWriteEvent(path, info) {
		t.Fatal("the first event was reported as a duplicate")
	}

	if err := os.Chtimes(path, info.ModTime().Add(time.Second), info.ModTime().Add(time.Second)); err != nil {
		t.Fatalf("failed to change the modification time: %v", err)
	}
	changed, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat the changed file: %v", err)
	}

	if isDuplicateWriteEvent(path, changed) {
		t.Error("a genuinely modified file was skipped as a duplicate")
	}
}

func TestIsDuplicateWriteEventPassesThroughAMissingFile(t *testing.T) {
	if isDuplicateWriteEvent(filepath.Join(t.TempDir(), "gone.json"), nil) {
		t.Error("an event with no file info was reported as a duplicate, which would drop the reload")
	}
}

func isolateReloadCallbacks(t *testing.T) {
	t.Helper()

	onReloadMux.Lock()
	previous := onReloadCallbacks
	onReloadCallbacks = nil
	onReloadMux.Unlock()

	t.Cleanup(func() {
		if onReloadMux.TryLock() {
			onReloadCallbacks = previous
			onReloadMux.Unlock()
		}
	})
}

func TestNotifyReloadCallbacksRunsCallbacksOutsideTheLock(t *testing.T) {
	isolateReloadCallbacks(t)

	called := 0
	OnReload(func() {
		called++
		OnReload(func() {})
	})

	done := make(chan struct{})
	go func() {
		notifyReloadCallbacks()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notifyReloadCallbacks deadlocked, so a callback cannot register another")
	}

	if called != 1 {
		t.Errorf("the callback ran %d times, want 1", called)
	}
}

func TestReloadWatchedFileReloadsTheConfigAndNotifies(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := loadConfig(); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}

	writeWatchedFile(t, configPath, `{"prefix":"?","language":"en","default_volume":50}`)

	isolateReloadCallbacks(t)

	notified := false
	OnReload(func() { notified = true })

	absPath, err := filepath.Abs(configPath)
	if err != nil {
		t.Fatalf("failed to resolve the config path: %v", err)
	}
	reloadWatchedFile(absPath)

	if got := GetConfig().Prefix; got != "?" {
		t.Errorf("got prefix %q, want %q", got, "?")
	}
	if !notified {
		t.Error("the reload callbacks were not run")
	}
}

func TestReloadWatchedFileIgnoresUnrelatedPaths(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := loadConfig(); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}
	before := GetConfig().Prefix

	writeWatchedFile(t, configPath, `{"prefix":"?","language":"en","default_volume":50}`)
	reloadWatchedFile(filepath.Join(t.TempDir(), "unrelated.json"))

	if got := GetConfig().Prefix; got != before {
		t.Errorf("got prefix %q, want it unchanged at %q", got, before)
	}
}
