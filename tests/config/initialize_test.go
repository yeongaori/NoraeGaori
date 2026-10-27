package config_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	"noraegaori/internal/config"
	"noraegaori/tests/testutil"
)

func assertInitFails(t *testing.T, want string) {
	t.Helper()

	err := config.Initialize()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Initialize() = %v, want an error containing %q", err, want)
	}
}

func TestInitializeNoConfigDir(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := os.WriteFile("config", []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to block the config directory: %v", err)
	}

	assertInitFails(t, "failed to create config directory")
}

func TestInitializeEmptyConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	writeWatchedFile(t, *config.HookConfigPath, "")

	assertInitFails(t, "config file is empty")
}

func TestInitializeCorruptAdmins(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	writeWatchedFile(t, *config.HookAdminsPath, "{not json")

	assertInitFails(t, "failed to parse admins file")
}

func TestInitializeNoWatcher(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	testutil.Swap(t, config.HookNewFileWatcher, func() (*fsnotify.Watcher, error) {
		return nil, errors.New("inotify watch limit reached")
	})

	assertInitFails(t, "failed to create file watcher")
}

func TestInitializeUnwatchedFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	setupTestConfig(t)
	defer teardownTestConfig(t)

	testutil.Swap(t, config.HookNewFileWatcher, func() (*fsnotify.Watcher, error) {
		fileWatcher, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, err
		}
		return fileWatcher, fileWatcher.Close()
	})

	if err := config.Initialize(); err != nil {
		t.Fatalf("Initialize() = %v with an unusable watcher, want the config loaded anyway", err)
	}
	if config.GetConfig() == nil {
		t.Error("the config was not loaded")
	}
}
