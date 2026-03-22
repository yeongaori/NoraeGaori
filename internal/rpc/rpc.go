package rpc

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/pkg/logger"
)

// ActivityType maps string types to Discord activity types
var ActivityTypeMap = map[string]discordgo.ActivityType{
	"Playing":   discordgo.ActivityTypeGame,
	"Streaming": discordgo.ActivityTypeStreaming,
	"Listening": discordgo.ActivityTypeListening,
	"Watching":  discordgo.ActivityTypeWatching,
	"Custom":    discordgo.ActivityTypeCustom,
	"Competing": discordgo.ActivityTypeCompeting,
}

// Activity represents a single RPC activity
type Activity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Config represents the RPC configuration
type Config struct {
	RPCEnabled         bool       `json:"RPC_ENABLED"`
	RPCIntervalSeconds int        `json:"RPC_INTERVAL_SECONDS"`
	LogRPCChanges      bool       `json:"LOG_RPC_CHANGES"`
	RandomizeRPC       bool       `json:"RANDOMIZE_RPC"`
	Activities         []Activity `json:"activities"`
}

// DefaultConfig returns the default RPC configuration
func DefaultConfig() *Config {
	return &Config{
		RPCEnabled:         true,
		RPCIntervalSeconds: 30,
		LogRPCChanges:      false,
		RandomizeRPC:       true,
		Activities: []Activity{
			{Name: messages.T().RPC.ActivityMusic, Type: "Playing"},
			{Name: messages.T().RPC.ActivitySong, Type: "Listening"},
			{Name: messages.T().RPC.ActivityPlaylist, Type: "Watching"},
			{Name: messages.T().RPC.ActivityMusicVideo, Type: "Watching"},
		},
	}
}

// LoadConfig loads RPC configuration from file
func LoadConfig() (*Config, error) {
	configPath := filepath.Join("config", "rpcConfig.json")

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Warn("rpcConfig.json not found. Creating default RPC config file.")

		// Create default config
		defaultCfg := DefaultConfig()
		data, err := json.MarshalIndent(defaultCfg, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal default config: %w", err)
		}

		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to write default config: %w", err)
		}

		logger.Info("Created default rpcConfig.json file.")
		return defaultCfg, nil
	}

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

var (
	stopChan chan bool
	running  bool
	runningMu sync.Mutex
)

// UpdateRPC starts the RPC update loop
func UpdateRPC(session *discordgo.Session) {
	runningMu.Lock()
	if running {
		runningMu.Unlock()
		logger.Warn("RPC update loop is already running")
		return
	}
	running = true
	stopChan = make(chan bool, 1)
	runningMu.Unlock()

	cfg, err := LoadConfig()
	if err != nil {
		logger.Warnf("Failed to load rpcConfig.json: %v", err)
		logger.Warn("RPC functionality will be disabled.")
		runningMu.Lock()
		running = false
		runningMu.Unlock()
		return
	}

	if !cfg.RPCEnabled {
		logger.Warn("RPC is disabled in the config.")
		runningMu.Lock()
		running = false
		runningMu.Unlock()
		return
	}

	if len(cfg.Activities) == 0 {
		logger.Error("No activities found in rpcConfig.json")
		runningMu.Lock()
		running = false
		runningMu.Unlock()
		return
	}

	currentIndex := 0
	ticker := time.NewTicker(time.Duration(cfg.RPCIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Update immediately
	updateActivity(session, cfg, &currentIndex)

	// Update on interval
	for {
		select {
		case <-ticker.C:
			updateActivity(session, cfg, &currentIndex)
		case <-stopChan:
			logger.Info("RPC update loop stopped")
			runningMu.Lock()
			running = false
			runningMu.Unlock()
			return
		}
	}
}

// Stop stops the RPC update loop
func Stop() {
	runningMu.Lock()
	defer runningMu.Unlock()

	if !running {
		return
	}

	if stopChan != nil {
		close(stopChan)
	}
}

// updateActivity updates the bot's activity status
func updateActivity(session *discordgo.Session, cfg *Config, currentIndex *int) {
	var activity Activity

	if cfg.RandomizeRPC {
		// Random selection
		activity = cfg.Activities[rand.Intn(len(cfg.Activities))]
	} else {
		// Sequential selection
		activity = cfg.Activities[*currentIndex]
		*currentIndex = (*currentIndex + 1) % len(cfg.Activities)
	}

	// Get Discord activity type
	activityType, ok := ActivityTypeMap[activity.Type]
	if !ok {
		logger.Warnf("Invalid activity type: %s", activity.Type)
		return
	}

	// Update presence
	err := session.UpdateStatusComplex(discordgo.UpdateStatusData{
		Activities: []*discordgo.Activity{
			{
				Name: activity.Name,
				Type: activityType,
			},
		},
		Status: "online",
	})

	if err != nil {
		logger.Warnf("Failed to update RPC: %v", err)
		return
	}

	if cfg.LogRPCChanges {
		logger.Infof("RPC updated to: %s %s", activity.Type, activity.Name)
	}
}
