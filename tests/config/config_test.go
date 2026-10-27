package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"noraegaori/internal/config"
	"noraegaori/tests/testutil"
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

func TestDefaultVolumeFallback(t *testing.T) {
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

	info, err := os.Stat(*config.HookAdminsPath)
	if err != nil {
		t.Fatalf("the default admins file is missing: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("got mode %v, want 0600 so only the bot user can read it", info.Mode().Perm())
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

	for i := range testCases {
		tc := &testCases[i]
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

	for i := range testCases {
		tc := &testCases[i]
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

func TestGetConfigConcurrent(t *testing.T) {
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

	info, err := os.Stat(*config.HookConfigPath)
	if err != nil {
		t.Fatalf("the saved config is missing: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("got mode %v, want 0600 so only the bot user can read it", info.Mode().Perm())
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

func loadYtDlpChannel(t *testing.T, body string) string {
	t.Helper()

	setupTestConfig(t)
	t.Cleanup(func() { teardownTestConfig(t) })

	if err := os.WriteFile(*config.HookConfigPath, []byte(body), 0644); err != nil {
		t.Fatalf("failed to seed the config: %v", err)
	}
	if err := config.HookLoadConfig(); err != nil {
		t.Fatalf("loadConfig returned %v, want nil", err)
	}
	return config.GetConfig().YtDlpChannel
}

func TestLoadConfigDefaultChannel(t *testing.T) {
	if got := loadYtDlpChannel(t, `{"prefix":"!","language":"en","default_volume":100}`); got != config.YtDlpChannelAuto {
		t.Errorf("got channel %q for a config without the key, want %q", got, config.YtDlpChannelAuto)
	}
}

func TestLoadConfigUnknownChannel(t *testing.T) {
	if got := loadYtDlpChannel(t, `{"prefix":"!","ytdlp_channel":"bleeding-edge"}`); got != config.YtDlpChannelAuto {
		t.Errorf("got channel %q, want the fallback %q", got, config.YtDlpChannelAuto)
	}
}

func TestLoadConfigStableChannel(t *testing.T) {
	if got := loadYtDlpChannel(t, `{"prefix":"!","ytdlp_channel":"stable"}`); got != config.YtDlpChannelStable {
		t.Errorf("got channel %q, want %q to be honoured", got, config.YtDlpChannelStable)
	}
}

func TestLoadConfigNightlyChannel(t *testing.T) {
	if got := loadYtDlpChannel(t, `{"prefix":"!","ytdlp_channel":"nightly"}`); got != config.YtDlpChannelNightly {
		t.Errorf("got channel %q, want %q to be honoured", got, config.YtDlpChannelNightly)
	}
}

func TestConfigDirMode(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Swap(t, config.HookConfigPath, filepath.Join("config", "config.json"))
	testutil.Swap(t, config.HookAdminsPath, filepath.Join("config", "admins.json"))
	previousConfig, previousAdmins := config.HookConfig.Load(), config.HookAdminsConf.Load()
	t.Cleanup(func() {
		config.HookConfig.Store(previousConfig)
		config.HookAdminsConf.Store(previousAdmins)
	})

	if err := config.Initialize(); err != nil {
		t.Fatalf("Initialize returned %v, want nil", err)
	}
	t.Cleanup(func() { _ = config.Close() })

	info, err := os.Stat("config")
	if err != nil {
		t.Fatalf("the config directory is missing: %v", err)
	}
	if info.Mode().Perm()&0007 != 0 {
		t.Errorf("config dir mode = %v, want no access for other users", info.Mode().Perm())
	}
}
