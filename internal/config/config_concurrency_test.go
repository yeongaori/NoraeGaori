package config

import (
	"sync"
	"testing"
)

func TestSetPrefixDoesNotMutateAPublishedConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := loadConfig(); err != nil {
		t.Fatalf("failed to load the default config: %v", err)
	}

	held := GetConfig()
	original := held.Prefix

	if err := SetPrefix("?"); err != nil {
		t.Fatalf("SetPrefix returned %v, want nil", err)
	}

	if held.Prefix != original {
		t.Errorf("a config already handed out was mutated in place from %q to %q", original, held.Prefix)
	}
	if GetConfig().Prefix != "?" {
		t.Errorf("got prefix %q from a fresh GetConfig, want %q", GetConfig().Prefix, "?")
	}
}

func TestSetPrefixRacesWithReaders(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := loadConfig(); err != nil {
		t.Fatalf("failed to load the default config: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				cfg := GetConfig()
				if cfg == nil {
					continue
				}
				_ = cfg.Prefix
				_ = cfg.Language
				_ = cfg.MaxDownloadSpeedMbps
			}
		}()
	}

	for i := 0; i < 40; i++ {
		if err := SetPrefix("!"); err != nil {
			t.Errorf("SetPrefix returned %v, want nil", err)
			break
		}
	}

	close(stop)
	wg.Wait()
}

func TestSetPrefixWithoutConfigReturnsError(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.Store(nil)

	if err := SetPrefix("!"); err == nil {
		t.Error("SetPrefix returned nil, want an error when the config is not initialized")
	}
}
