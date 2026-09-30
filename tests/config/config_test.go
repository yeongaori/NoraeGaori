package config_test

import (
	"os"
	"testing"

	"noraegaori/internal/config"
)

func setupTestConfig(t *testing.T) {
	os.MkdirAll("test_config", 0755)
	*config.HookConfigPath = "test_config/config.json"
	*config.HookAdminsPath = "test_config/admins.json"
}

func teardownTestConfig(t *testing.T) {
	os.RemoveAll("test_config")
	config.HookConfig.Store(nil)
	config.HookAdminsConf.Store(nil)
}

func TestDefaultVolumeFallsBackWithoutAConfig(t *testing.T) {
	previous := config.HookConfig.Load()
	t.Cleanup(func() { config.HookConfig.Store(previous) })

	config.HookConfig.Store(nil)
	if got := config.DefaultVolume(); got != 100 {
		t.Errorf("DefaultVolume() = %g without a config, want 100", got)
	}

	config.HookConfig.Store(&config.Config{DefaultVolume: 42})
	if got := config.DefaultVolume(); got != 42 {
		t.Errorf("DefaultVolume() = %g, want the configured 42", got)
	}
}

func TestLoadDefaultConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	if config.HookConfig.Load() == nil {
		t.Fatal("Config should not be nil")
	}

	if config.HookConfig.Load().Prefix == "" {
		t.Error("Default prefix should not be empty")
	}

	if config.HookConfig.Load().DefaultVolume <= 0 {
		t.Error("Default volume should be positive")
	}
}

func TestLoadDefaultAdmins(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := config.HookLoadAdmins(); err != nil {
		t.Fatalf("Failed to load default admins: %v", err)
	}

	if config.HookAdminsConf.Load() == nil {
		t.Fatal("Admins config should not be nil")
	}
}

func TestSetPrefix(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.HookLoadConfig()

	testCases := []struct {
		name      string
		prefix    string
		expectErr bool
	}{
		{"Valid single char", "!", false},
		{"Valid double char", "!!", false},
		{"Valid special char", "?", false},
		{"Valid dot", ".", false},
		{"Valid arrow", ">", false},
		{"Empty prefix", "", false},
		{"Long prefix", "verylongprefix", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := config.SetPrefix(tc.prefix)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectErr {
				cfg := config.GetConfig()
				if cfg.Prefix != tc.prefix {
					t.Errorf("Expected prefix %s, got %s", tc.prefix, cfg.Prefix)
				}
			}
		})
	}
}

func TestIsAdmin(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.HookAdminsConf.Store(&config.AdminsConfig{
		Admins: []string{"admin1", "admin2", "admin3"},
	})

	testCases := []struct {
		name     string
		userID   string
		expected bool
	}{
		{"Admin user 1", "admin1", true},
		{"Admin user 2", "admin2", true},
		{"Non-admin user", "user123", false},
		{"Empty user ID", "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := config.IsAdmin(tc.userID)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.HookLoadConfig()

	cfg := config.GetConfig()
	if cfg == nil {
		t.Fatal("GetConfig should not return nil")
	}

	if cfg.Prefix == "" {
		t.Error("Config prefix should not be empty")
	}
}

func TestGetAdmins(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.HookAdminsConf.Store(&config.AdminsConfig{
		Admins: []string{"admin1", "admin2"},
	})

	admins := config.GetAdmins()
	if len(admins) != 2 {
		t.Errorf("Expected 2 admins, got %d", len(admins))
	}
}

func TestConcurrentConfigAccess(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.HookLoadConfig()

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			cfg := config.GetConfig()
			if cfg == nil {
				t.Error("GetConfig returned nil")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		go func(idx int) {
			config.SetPrefix("!")
			done <- true
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	config.HookConfig.Store(&config.Config{
		Prefix:           "?",
		ShowStartedTrack: false,
		DefaultVolume:    75,
	})

	if err := config.HookSaveConfig(config.HookConfig.Load()); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	config.HookConfig.Store(nil)

	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if config.HookConfig.Load().Prefix != "?" {
		t.Errorf("Expected prefix ?, got %s", config.HookConfig.Load().Prefix)
	}
	if config.HookConfig.Load().ShowStartedTrack != false {
		t.Error("ShowStartedTrack should be false")
	}
	if config.HookConfig.Load().DefaultVolume != 75 {
		t.Errorf("Expected volume 75, got %g", config.HookConfig.Load().DefaultVolume)
	}
}

func TestNilAdminsConfig(t *testing.T) {
	config.HookAdminsConf.Store(nil)

	admins := config.GetAdmins()
	if admins == nil {
		t.Error("GetAdmins should return empty slice, not nil")
	}
	if len(admins) != 0 {
		t.Error("GetAdmins should return empty slice when adminsConf is nil")
	}

	result := config.IsAdmin("any_user")
	if result {
		t.Error("IsAdmin should return false when adminsConf is nil")
	}
}

func TestLoadConfigDefaultsTheYtDlpChannel(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := os.WriteFile(*config.HookConfigPath, []byte(`{"prefix":"!","language":"en","default_volume":100}`), 0644); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}
	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("loadConfig returned %v, want nil", err)
	}

	if got := config.GetConfig().YtDlpChannel; got != config.YtDlpChannelAuto {
		t.Errorf("got channel %q for a config without the key, want %q", got, config.YtDlpChannelAuto)
	}
}

func TestLoadConfigRejectsAnUnknownYtDlpChannel(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := os.WriteFile(*config.HookConfigPath, []byte(`{"prefix":"!","ytdlp_channel":"bleeding-edge"}`), 0644); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}
	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("loadConfig returned %v, want nil", err)
	}

	if got := config.GetConfig().YtDlpChannel; got != config.YtDlpChannelAuto {
		t.Errorf("got channel %q, want the fallback %q", got, config.YtDlpChannelAuto)
	}
}

func TestLoadConfigKeepsAnExplicitStableChannel(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := os.WriteFile(*config.HookConfigPath, []byte(`{"prefix":"!","ytdlp_channel":"stable"}`), 0644); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}
	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("loadConfig returned %v, want nil", err)
	}

	if got := config.GetConfig().YtDlpChannel; got != config.YtDlpChannelStable {
		t.Errorf("got channel %q, want %q to be honoured", got, config.YtDlpChannelStable)
	}
}
