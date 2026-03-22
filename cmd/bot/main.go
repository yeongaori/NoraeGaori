package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/joho/godotenv"
	"noraegaori/internal/bot"
	"noraegaori/internal/commands"
	"noraegaori/internal/config"
	"noraegaori/internal/database"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/youtube"
	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"
)

func main() {
	debug.SetGCPercent(300)

	// Load environment variables
	if err := loadEnv(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	// Initialize logger
	debugMode := os.Getenv("DEBUG_MODE") == "true"
	logger.Initialize(debugMode)
	defer logger.Close()

	// Initialize database
	logger.Info("Initializing database...")
	if err := database.Initialize(); err != nil {
		logger.Errorf("Failed to initialize database: %v", err)
		os.Exit(1)
	}
	defer database.Close()

	// Clear stale playing/loading states from database (bot restart cleanup)
	logger.Info("Clearing stale playback states...")
	if err := clearStalePlaybackStates(); err != nil {
		logger.Warnf("Failed to clear stale states: %v", err)
	}

	// Initialize configuration (must be before locale loading to get language setting)
	logger.Info("Loading configuration...")
	if err := config.Initialize(); err != nil {
		logger.Errorf("Failed to initialize config: %v", err)
		os.Exit(1)
	}
	defer config.Close()

	// Load locale strings based on configured language
	lang := config.GetConfig().Language
	logger.Infof("Loading locale: %s", lang)
	if err := messages.LoadLocale(lang); err != nil {
		logger.Warnf("Locale loading issue: %v", err)
	}

	// Reload locale when language changes via hot-reload
	lastLang := lang
	config.OnReload(func() {
		newLang := config.GetConfig().Language
		if newLang == lastLang {
			return
		}
		lastLang = newLang
		logger.Infof("Language changed to %q, reloading locale...", newLang)
		if err := messages.LoadLocale(newLang); err != nil {
			logger.Warnf("Locale reload issue: %v", err)
		}
		commands.ReloadAliases()
	})

	// Initialize YouTube integration
	logger.Info("Initializing YouTube integration...")
	if err := youtube.Initialize(); err != nil {
		logger.Errorf("Failed to initialize YouTube: %v", err)
		os.Exit(1)
	}

	// Update yt-dlp (always check on startup)
	ytdlpUpdater.AutoUpdate()

	// Get bot token
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		logger.Error("DISCORD_BOT_TOKEN is not set in environment variables")
		os.Exit(1)
	}

	// Start bot
	logger.Debugf("Opus encoder: %s", player.GetEncoderType())
	logger.Info("Starting Discord bot...")
	if err := bot.Start(token); err != nil {
		logger.Errorf("Failed to start bot: %v", err)
		os.Exit(1)
	}

	logger.Info("Bot stopped gracefully")
}

// clearStalePlaybackStates clears playing/loading flags from database on bot restart
func clearStalePlaybackStates() error {
	// When the bot restarts, any queues with playing=1 or loading=1 are stale
	// since there's no actual playback happening
	_, err := database.DB.Exec(`UPDATE queues SET playing = 0, loading = 0 WHERE playing = 1 OR loading = 1`)
	if err != nil {
		return fmt.Errorf("failed to clear stale states: %w", err)
	}
	logger.Info("Cleared stale playback states from database")
	return nil
}

// loadEnv loads environment variables from .env file
func loadEnv() error {
	envPath := ".env"

	// Check if .env exists
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		// Create example .env
		exampleEnv := `# Discord Bot Configuration
DISCORD_BOT_TOKEN=your_bot_token_here

# Optional: Debug mode
DEBUG_MODE=false
`
		if err := os.WriteFile(envPath, []byte(exampleEnv), 0644); err != nil {
			return fmt.Errorf("failed to create .env file: %w", err)
		}
		logger.Info("Created example .env file. Please configure it with your bot token.")
		return fmt.Errorf(".env file created - please add your bot token and restart")
	}

	// Load .env
	if err := godotenv.Load(envPath); err != nil {
		return fmt.Errorf("failed to load .env: %w", err)
	}

	return nil
}
