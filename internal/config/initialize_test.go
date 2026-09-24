package config

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	"noraegaori/tests/testutil"
)

func assertInitializeFails(t *testing.T, want string) {
	t.Helper()

	err := Initialize()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Initialize() = %v, want an error containing %q", err, want)
	}
}

func TestInitializeFailsWhenTheConfigDirectoryCannotBeCreated(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := os.WriteFile("config", []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to block the config directory: %v", err)
	}

	assertInitializeFails(t, "failed to create config directory")
}

func TestInitializeFailsOnAnEmptyConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	writeWatchedFile(t, configPath, "")

	assertInitializeFails(t, "config file is empty")
}

func TestInitializeFailsOnACorruptAdminsFile(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	writeWatchedFile(t, adminsPath, "{not json")

	assertInitializeFails(t, "failed to parse admins file")
}

func TestInitializeFailsWhenNoFileWatcherCanBeCreated(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	testutil.Swap(t, &newFileWatcher, func() (*fsnotify.Watcher, error) {
		return nil, errors.New("inotify watch limit reached")
	})

	assertInitializeFails(t, "failed to create file watcher")
}

func TestInitializeStillLoadsWhenTheFilesCannotBeWatched(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	testutil.Swap(t, &newFileWatcher, func() (*fsnotify.Watcher, error) {
		fileWatcher, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, err
		}
		return fileWatcher, fileWatcher.Close()
	})

	if err := Initialize(); err != nil {
		t.Fatalf("Initialize() = %v with an unusable watcher, want the config loaded anyway", err)
	}
	if GetConfig() == nil {
		t.Error("the config was not loaded")
	}
}
