package config

import (
	"os"
	"testing"
)

func setupTestConfig(t *testing.T) {
	os.MkdirAll("test_config", 0755)
	configPath = "test_config/config.json"
	adminsPath = "test_config/admins.json"
}

func teardownTestConfig(t *testing.T) {
	os.RemoveAll("test_config")
	config = nil
	adminsConf = nil
}

func TestLoadDefaultConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := loadConfig(); err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	if config == nil {
		t.Fatal("Config should not be nil")
	}

	if config.Prefix == "" {
		t.Error("Default prefix should not be empty")
	}

	if config.DefaultVolume <= 0 {
		t.Error("Default volume should be positive")
	}
}

func TestLoadDefaultAdmins(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	if err := loadAdmins(); err != nil {
		t.Fatalf("Failed to load default admins: %v", err)
	}

	if adminsConf == nil {
		t.Fatal("Admins config should not be nil")
	}
}

func TestSetPrefix(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	loadConfig()

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
		{"Empty prefix", "", false}, // Should be handled by command validation
		{"Long prefix", "verylongprefix", false}, // Should be handled by command validation
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := SetPrefix(tc.prefix)
			if tc.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectErr {
				cfg := GetConfig()
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

	// Create admins config
	adminsConf = &AdminsConfig{
		Admins: []string{"admin1", "admin2", "admin3"},
	}

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
			result := IsAdmin(tc.userID)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	loadConfig()

	cfg := GetConfig()
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

	adminsConf = &AdminsConfig{
		Admins: []string{"admin1", "admin2"},
	}

	admins := GetAdmins()
	if len(admins) != 2 {
		t.Errorf("Expected 2 admins, got %d", len(admins))
	}
}

func TestConcurrentConfigAccess(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	loadConfig()

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			cfg := GetConfig()
			if cfg == nil {
				t.Error("GetConfig returned nil")
			}
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(idx int) {
			SetPrefix("!")
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	setupTestConfig(t)
	defer teardownTestConfig(t)

	// Create initial config
	config = &Config{
		Prefix:           "?",
		ShowStartedTrack: false,
		DefaultVolume:    75,
	}

	// Save config
	if err := saveConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Reset config
	config = nil

	// Load config
	if err := loadConfig(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded values
	if config.Prefix != "?" {
		t.Errorf("Expected prefix ?, got %s", config.Prefix)
	}
	if config.ShowStartedTrack != false {
		t.Error("ShowStartedTrack should be false")
	}
	if config.DefaultVolume != 75 {
		t.Errorf("Expected volume 75, got %g", config.DefaultVolume)
	}
}

func TestNilAdminsConfig(t *testing.T) {
	adminsConf = nil

	admins := GetAdmins()
	if admins == nil {
		t.Error("GetAdmins should return empty slice, not nil")
	}
	if len(admins) != 0 {
		t.Error("GetAdmins should return empty slice when adminsConf is nil")
	}

	result := IsAdmin("any_user")
	if result {
		t.Error("IsAdmin should return false when adminsConf is nil")
	}
}
