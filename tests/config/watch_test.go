package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"noraegaori/internal/config"
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

	if config.HookIsDuplicateWriteEvent(path, info) {
		t.Error("the first event for a path was reported as a duplicate")
	}
	if !config.HookIsDuplicateWriteEvent(path, info) {
		t.Error("a repeat event with the same ModTime was not reported as a duplicate")
	}

	other := filepath.Join(t.TempDir(), "admins.json")
	otherInfo := writeWatchedFile(t, other, "{}")
	if config.HookIsDuplicateWriteEvent(other, otherInfo) {
		t.Error("a different path was reported as a duplicate")
	}
}

func TestIsDuplicateWriteEventAcceptsAChangedModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	info := writeWatchedFile(t, path, "{}")

	if config.HookIsDuplicateWriteEvent(path, info) {
		t.Fatal("the first event was reported as a duplicate")
	}

	if err := os.Chtimes(path, info.ModTime().Add(time.Second), info.ModTime().Add(time.Second)); err != nil {
		t.Fatalf("failed to change the modification time: %v", err)
	}
	changed, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat the changed file: %v", err)
	}

	if config.HookIsDuplicateWriteEvent(path, changed) {
		t.Error("a genuinely modified file was skipped as a duplicate")
	}
}

func TestIsDuplicateWriteEventPassesThroughAMissingFile(t *testing.T) {
	if config.HookIsDuplicateWriteEvent(filepath.Join(t.TempDir(), "gone.json"), nil) {
		t.Error("an event with no file info was reported as a duplicate, which would drop the reload")
	}
}

func isolateReloadCallbacks(t *testing.T) {
	t.Helper()

	config.HookOnReloadMux.Lock()
	previous := *config.HookOnReloadCallbacks
	*config.HookOnReloadCallbacks = nil
	config.HookOnReloadMux.Unlock()

	t.Cleanup(func() {
		if config.HookOnReloadMux.TryLock() {
			*config.HookOnReloadCallbacks = previous
			config.HookOnReloadMux.Unlock()
		}
	})
}

func TestNotifyReloadCallbacksRunsCallbacksOutsideTheLock(t *testing.T) {
	isolateReloadCallbacks(t)

	called := 0
	config.OnReload(func() {
		called++
		config.OnReload(func() {})
	})

	done := make(chan struct{})
	go func() {
		config.HookNotifyReloadCallbacks()
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

	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}

	writeWatchedFile(t, *config.HookConfigPath, `{"prefix":"?","language":"en","default_volume":50}`)

	isolateReloadCallbacks(t)

	notified := false
	config.OnReload(func() { notified = true })

	absPath, err := filepath.Abs(*config.HookConfigPath)
	if err != nil {
		t.Fatalf("failed to resolve the config path: %v", err)
	}
	config.HookReloadWatchedFile(absPath)

	if got := config.GetConfig().Prefix; got != "?" {
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

	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}
	before := config.GetConfig().Prefix

	writeWatchedFile(t, *config.HookConfigPath, `{"prefix":"?","language":"en","default_volume":50}`)
	config.HookReloadWatchedFile(filepath.Join(t.TempDir(), "unrelated.json"))

	if got := config.GetConfig().Prefix; got != before {
		t.Errorf("got prefix %q, want it unchanged at %q", got, before)
	}
}

func waitForWatcherToStop(t *testing.T, fileWatcher *fsnotify.Watcher, stop func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		config.HookWatchFiles(fileWatcher)
		close(done)
	}()

	stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchFiles kept running after its watcher closed")
	}
}

func TestWatchFilesLogsErrorsAndStopsWhenEitherChannelCloses(t *testing.T) {
	eventsClosing := &fsnotify.Watcher{Events: make(chan fsnotify.Event), Errors: make(chan error)}
	waitForWatcherToStop(t, eventsClosing, func() {
		eventsClosing.Errors <- errors.New("watch failed")
		close(eventsClosing.Events)
	})

	errorsClosing := &fsnotify.Watcher{Events: make(chan fsnotify.Event), Errors: make(chan error)}
	waitForWatcherToStop(t, errorsClosing, func() {
		close(errorsClosing.Errors)
	})
}

func TestInitializeReloadsTheConfigWhenTheFileChanges(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)
	isolateReloadCallbacks(t)

	reloaded := make(chan struct{}, 1)
	config.OnReload(func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})

	if err := config.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer config.Close()

	writeWatchedFile(t, *config.HookConfigPath, `{"prefix":"?","language":"en","default_volume":50}`)

	select {
	case <-reloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("the file watcher did not reload the changed config")
	}
	if got := config.GetConfig().Prefix; got != "?" {
		t.Errorf("got prefix %q after the watched reload, want %q", got, "?")
	}
}
