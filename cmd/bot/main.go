package main

import (
	"context"
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
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	ytdlpUpdater "noraegaori/internal/ytdlp"
	"noraegaori/pkg/logger"
)

func main() {
	debug.SetGCPercent(300)

	
	if err := loadEnv(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	
	debugMode := os.Getenv("DEBUG_MODE") == "true"
	logger.Initialize(debugMode)
	defer logger.Close()

	
	logger.Info("Initializing database...")
	if err := database.Initialize(); err != nil {
		logger.Errorf("Failed to initialize database: %v", err)
		os.Exit(1)
	}
	defer database.Close()

	
	logger.Info("Clearing stale playback states...")
	if err := clearStalePlaybackStates(); err != nil {
		logger.Warnf("Failed to clear stale states: %v", err)
	}

	
	logger.Info("Loading configuration...")
	if err := config.Initialize(); err != nil {
		logger.Errorf("Failed to initialize config: %v", err)
		os.Exit(1)
	}
	defer config.Close()

	
	
	
	messages.SetGuildLangResolver(queue.GetGuildLanguage)

	
	lang := config.GetConfig().Language
	logger.Infof("Loading locale: %s", lang)
	if err := messages.LoadLocale(lang); err != nil {
		logger.Warnf("Locale loading issue: %v", err)
	}

	
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

	
	logger.Info("Initializing yt-dlp version manager...")
	if err := ytdlpUpdater.InitVersionManager(); err != nil {
		logger.Warnf("Failed to initialize version manager: %v", err)
	}

	
	logger.Info("Initializing YouTube integration...")
	if err := youtube.Initialize(); err != nil {
		logger.Errorf("Failed to initialize YouTube: %v", err)
		os.Exit(1)
	}

	
	ytdlpUpdater.AutoUpdate()
	ytdlpUpdater.DetectJsRuntime()

	
	updaterCtx, updaterCancel := context.WithCancel(context.Background())
	defer updaterCancel()
	ytdlpUpdater.StartBackgroundUpdater(updaterCtx)

	
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		logger.Error("DISCORD_BOT_TOKEN is not set in environment variables")
		os.Exit(1)
	}

	
	logger.Debugf("Opus encoder: %s", player.GetEncoderType())
	logger.Info("Starting Discord bot...")
	if err := bot.Start(token); err != nil {
		logger.Errorf("Failed to start bot: %v", err)
		os.Exit(1)
	}

	logger.Info("Bot stopped gracefully")
}

func clearStalePlaybackStates() error {
	
	
	_, err := database.DB.Exec(`UPDATE queues SET playing = 0, loading = 0 WHERE playing = 1 OR loading = 1`)
	if err != nil {
		return fmt.Errorf("failed to clear stale states: %w", err)
	}
	logger.Info("Cleared stale playback states from database")
	return nil
}

func loadEnv() error {
	envPath := ".env"

	
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		
		exampleEnv := `# Discord Bot Configuration
DISCORD_BOT_TOKEN=your_bot_token_here

# Optional: Debug mode
DEBUG_MODE=false
`
		if err := os.WriteFile(envPath, []byte(exampleEnv), 0644); err != nil {
			return fmt.Errorf("failed to create .env file: %w", err)
		}
		logger.Warn("Created example .env file. Please configure it with your bot token.")
		return fmt.Errorf(".env file created - please add your bot token and restart")
	}

	
	if err := godotenv.Load(envPath); err != nil {
		return fmt.Errorf("failed to load .env: %w", err)
	}

	return nil
}
