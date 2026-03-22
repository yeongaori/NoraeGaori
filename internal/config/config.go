package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"noraegaori/pkg/logger"
)

// Config represents the bot configuration
type Config struct {
	Prefix              string  `json:"prefix"`
	Language            string  `json:"language"`               // Locale code (e.g., "ko", "en"). Default: "ko"
	ShowStartedTrack    bool    `json:"show_started_track"`
	DefaultVolume       float64 `json:"default_volume"`
	PreCacheStrategy    int     `json:"precache_strategy"`     // 0=None, 1=FullMemory, 3=RangeReq
	MaxPreCacheMemory   float64 `json:"max_precache_memory"`   // Max memory in GB (default: 1.0)
	MaxDownloadSpeedMbps float64 `json:"max_download_speed_mbps"` // Max download speed in Mbps per server (default: 10.0)
}

// AdminsConfig represents the admin users configuration
type AdminsConfig struct {
	Admins []string `json:"admins"`
}

var (
	config      *Config
	adminsConf  *AdminsConfig
	configMux   sync.RWMutex
	adminsMux   sync.RWMutex
	watcher     *fsnotify.Watcher
	configPath  = "config/config.json"
	adminsPath  = "config/admins.json"
	lastModTime = make(map[string]int64) // Track last processed modification time per file
	modTimeMux  sync.RWMutex

	onReloadCallbacks []func()
	onReloadMux       sync.Mutex
)

// OnReload registers a callback that is invoked after config.json is successfully reloaded.
func OnReload(fn func()) {
	onReloadMux.Lock()
	defer onReloadMux.Unlock()
	onReloadCallbacks = append(onReloadCallbacks, fn)
}

// Initialize loads configuration files and sets up file watchers
func Initialize() error {
	// Ensure config directory exists
	if err := os.MkdirAll("config", 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Load config files
	if err := loadConfig(); err != nil {
		return err
	}
	if err := loadAdmins(); err != nil {
		return err
	}

	// Set up file watcher
	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Watch config files
	if err := watcher.Add(configPath); err != nil {
		logger.Warnf("Failed to watch config file: %v", err)
	}
	if err := watcher.Add(adminsPath); err != nil {
		logger.Warnf("Failed to watch admins file: %v", err)
	}

	// Start watching for changes
	go watchFiles()

	logger.Debugf("Configuration system initialized")
	return nil
}

// loadConfig loads the config.json file
func loadConfig() error {
	// Create default config if it doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := &Config{
			Prefix:              "!",
			Language:            "en",
			ShowStartedTrack:    true,
			DefaultVolume:       100,
			PreCacheStrategy:    1,    // Default: Full Memory pre-caching
			MaxPreCacheMemory:   1.0,  // Default: 1 GB
			MaxDownloadSpeedMbps: 10.0, // Default: 10 Mbps per server
		}
		if err := saveConfig(defaultConfig); err != nil {
			return fmt.Errorf("failed to create default config: %w", err)
		}
		logger.Info("Created default config.json file")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Check for empty or truncated file
	if len(data) == 0 {
		logger.Warnf("Config file is empty, using defaults")
		return fmt.Errorf("config file is empty")
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// JSON parse error - likely corrupted file
		logger.Errorf("Failed to parse config file (corrupted JSON): %v", err)
		logger.Warnf("Config file may be corrupted. Please check %s", configPath)
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate precache_strategy and fallback to default if invalid
	if cfg.PreCacheStrategy != 0 && cfg.PreCacheStrategy != 1 && cfg.PreCacheStrategy != 3 {
		logger.Warnf("Invalid precache_strategy=%d, falling back to default (1=FullMemory)", cfg.PreCacheStrategy)
		cfg.PreCacheStrategy = 1
	}

	// Validate max_precache_memory and fallback to default if invalid
	if cfg.MaxPreCacheMemory <= 0 || math.IsNaN(cfg.MaxPreCacheMemory) || math.IsInf(cfg.MaxPreCacheMemory, 0) {
		logger.Warnf("Invalid max_precache_memory=%.2f, falling back to default (1.0 GB)", cfg.MaxPreCacheMemory)
		cfg.MaxPreCacheMemory = 1.0
	}

	// Validate max_download_speed_mbps and fallback to default if invalid
	if cfg.MaxDownloadSpeedMbps <= 0 || math.IsNaN(cfg.MaxDownloadSpeedMbps) || math.IsInf(cfg.MaxDownloadSpeedMbps, 0) {
		logger.Warnf("Invalid max_download_speed_mbps=%.2f, falling back to default (10.0 Mbps)", cfg.MaxDownloadSpeedMbps)
		cfg.MaxDownloadSpeedMbps = 10.0
	}

	// Validate language and fallback to default if empty
	if cfg.Language == "" {
		cfg.Language = "en"
	}

	configMux.Lock()
	config = &cfg
	configMux.Unlock()

	logger.Infof("Loaded config: prefix=%s, language=%s, volume=%g, precache_strategy=%d, max_precache_memory=%.2fGB, max_download_speed=%.1fMbps",
		cfg.Prefix, cfg.Language, cfg.DefaultVolume, cfg.PreCacheStrategy, cfg.MaxPreCacheMemory, cfg.MaxDownloadSpeedMbps)
	return nil
}

// loadAdmins loads the admins.json file
func loadAdmins() error {
	// Create default admins file if it doesn't exist
	if _, err := os.Stat(adminsPath); os.IsNotExist(err) {
		defaultAdmins := &AdminsConfig{
			Admins: []string{},
		}
		if err := saveAdmins(defaultAdmins); err != nil {
			return fmt.Errorf("failed to create default admins config: %w", err)
		}
		logger.Info("Created default admins.json file")
	}

	data, err := os.ReadFile(adminsPath)
	if err != nil {
		return fmt.Errorf("failed to read admins file: %w", err)
	}

	var admins AdminsConfig
	if err := json.Unmarshal(data, &admins); err != nil {
		return fmt.Errorf("failed to parse admins file: %w", err)
	}

	adminsMux.Lock()
	adminsConf = &admins
	adminsMux.Unlock()

	logger.Infof("Loaded %d admin users", len(admins.Admins))
	return nil
}

// saveConfig saves the config to file
func saveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// saveAdmins saves the admins to file
func saveAdmins(admins *AdminsConfig) error {
	data, err := json.MarshalIndent(admins, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(adminsPath, data, 0644)
}

// watchFiles watches for configuration file changes
func watchFiles() {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Log ALL events with file details to diagnose double reload
			fileInfo, err := os.Stat(event.Name)
			if err == nil {
				logger.Debugf("[Config Watcher] Event: %s | Op: %s | File: %s | Size: %d | ModTime: %v",
					event.String(), event.Op.String(), event.Name, fileInfo.Size(), fileInfo.ModTime().UnixNano())
			} else {
				logger.Debugf("[Config Watcher] Event: %s | Op: %s | File: %s | (stat error: %v)",
					event.String(), event.Op.String(), event.Name, err)
			}

			if event.Op&fsnotify.Write == fsnotify.Write {
				absPath, _ := filepath.Abs(event.Name)
				configAbsPath, _ := filepath.Abs(configPath)
				adminsAbsPath, _ := filepath.Abs(adminsPath)

				// Check if file modification time has changed since last reload
				if fileInfo != nil {
					currentModTime := fileInfo.ModTime().UnixNano()

					modTimeMux.RLock()
					lastMod, exists := lastModTime[absPath]
					modTimeMux.RUnlock()

					// Skip if this is a duplicate event (same modification time)
					if exists && lastMod == currentModTime {
						logger.Debugf("[Config Watcher] Skipping duplicate event for %s (same ModTime)", event.Name)
						continue
					}

					// Update last modification time
					modTimeMux.Lock()
					lastModTime[absPath] = currentModTime
					modTimeMux.Unlock()
				}

				if absPath == configAbsPath {
					if err := loadConfig(); err != nil {
						logger.Errorf("Failed to reload config: %v", err)
					} else {
						logger.Info("Configuration reloaded")
						// Notify registered callbacks (e.g., locale reload)
						onReloadMux.Lock()
						cbs := make([]func(), len(onReloadCallbacks))
						copy(cbs, onReloadCallbacks)
						onReloadMux.Unlock()
						for _, fn := range cbs {
							fn()
						}
					}
				} else if absPath == adminsAbsPath {
					if err := loadAdmins(); err != nil {
						logger.Errorf("Failed to reload admins: %v", err)
					} else {
						logger.Info("Admins configuration reloaded")
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Errorf("File watcher error: %v", err)
		}
	}
}

// GetConfig returns the current configuration (thread-safe)
func GetConfig() *Config {
	configMux.RLock()
	defer configMux.RUnlock()
	return config
}

// SetPrefix updates the prefix and saves to file
func SetPrefix(prefix string) error {
	configMux.Lock()
	defer configMux.Unlock()

	config.Prefix = prefix

	if err := saveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	logger.Infof("[Config] Prefix updated to: %s", prefix)
	return nil
}

// GetAdmins returns the current admin list (thread-safe)
func GetAdmins() []string {
	adminsMux.RLock()
	defer adminsMux.RUnlock()
	if adminsConf == nil {
		return []string{}
	}
	return adminsConf.Admins
}

// IsAdmin checks if a user ID is in the admin list
func IsAdmin(userID string) bool {
	adminsMux.RLock()
	defer adminsMux.RUnlock()
	if adminsConf == nil {
		return false
	}
	for _, adminID := range adminsConf.Admins {
		if adminID == userID {
			return true
		}
	}
	return false
}

// Close closes the file watcher
func Close() error {
	if watcher != nil {
		return watcher.Close()
	}
	return nil
}
