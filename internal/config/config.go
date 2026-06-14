package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"noraegaori/pkg/logger"

	"github.com/fsnotify/fsnotify"
)

type Config struct {
	Prefix               string  `json:"prefix"`
	Language             string  `json:"language"`
	ShowStartedTrack     bool    `json:"show_started_track"`
	DefaultVolume        float64 `json:"default_volume"`
	MaxDownloadSpeedMbps float64 `json:"max_download_speed_mbps"`
	LogFile              string  `json:"log_file"`
}

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
	lastModTime = make(map[string]int64)
	modTimeMux  sync.RWMutex

	onReloadCallbacks []func()
	onReloadMux       sync.Mutex
)

func OnReload(fn func()) {
	onReloadMux.Lock()
	defer onReloadMux.Unlock()
	onReloadCallbacks = append(onReloadCallbacks, fn)
}

func Initialize() error {

	if err := os.MkdirAll("config", 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := loadConfig(); err != nil {
		return err
	}
	if err := loadAdmins(); err != nil {
		return err
	}

	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	if err := watcher.Add(configPath); err != nil {
		logger.Warnf("Failed to watch config file: %v", err)
	}
	if err := watcher.Add(adminsPath); err != nil {
		logger.Warnf("Failed to watch admins file: %v", err)
	}

	go watchFiles()

	logger.Debugf("Configuration system initialized")
	return nil
}

func loadConfig() error {

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := &Config{
			Prefix:               "!",
			Language:             "en",
			ShowStartedTrack:     true,
			DefaultVolume:        100,
			MaxDownloadSpeedMbps: 10.0,
			LogFile:              "latest.log",
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

	if len(data) == 0 {
		logger.Warnf("Config file is empty, using defaults")
		return fmt.Errorf("config file is empty")
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {

		logger.Errorf("Failed to parse config file (corrupted JSON): %v", err)
		logger.Warnf("Config file may be corrupted. Please check %s", configPath)
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.MaxDownloadSpeedMbps <= 0 || math.IsNaN(cfg.MaxDownloadSpeedMbps) || math.IsInf(cfg.MaxDownloadSpeedMbps, 0) {
		logger.Warnf("Invalid max_download_speed_mbps=%.2f, falling back to default (10.0 Mbps)", cfg.MaxDownloadSpeedMbps)
		cfg.MaxDownloadSpeedMbps = 10.0
	}

	if cfg.Language == "" {
		cfg.Language = "en"
	}

	if cfg.LogFile == "" {
		cfg.LogFile = "latest.log"
	}

	configMux.Lock()
	config = &cfg
	configMux.Unlock()

	logger.Infof("Loaded config: prefix=%s, language=%s, volume=%g, max_download_speed=%.1fMbps",
		cfg.Prefix, cfg.Language, cfg.DefaultVolume, cfg.MaxDownloadSpeedMbps)
	return nil
}

func loadAdmins() error {

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

func saveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func saveAdmins(admins *AdminsConfig) error {
	data, err := json.MarshalIndent(admins, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(adminsPath, data, 0644)
}

func watchFiles() {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

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

				if fileInfo != nil {
					currentModTime := fileInfo.ModTime().UnixNano()

					modTimeMux.RLock()
					lastMod, exists := lastModTime[absPath]
					modTimeMux.RUnlock()

					if exists && lastMod == currentModTime {
						logger.Debugf("[Config Watcher] Skipping duplicate event for %s (same ModTime)", event.Name)
						continue
					}

					modTimeMux.Lock()
					lastModTime[absPath] = currentModTime
					modTimeMux.Unlock()
				}

				if absPath == configAbsPath {
					if err := loadConfig(); err != nil {
						logger.Errorf("Failed to reload config: %v", err)
					} else {
						logger.Info("Configuration reloaded")

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

func GetConfig() *Config {
	configMux.RLock()
	defer configMux.RUnlock()
	return config
}

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

func GetAdmins() []string {
	adminsMux.RLock()
	defer adminsMux.RUnlock()
	if adminsConf == nil {
		return []string{}
	}
	return adminsConf.Admins
}

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

func Close() error {
	if watcher != nil {
		return watcher.Close()
	}
	return nil
}
