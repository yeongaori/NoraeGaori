package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"noraegaori/internal/app"
	"noraegaori/internal/logger"

	"github.com/joho/godotenv"
)

func main() {
	debug.SetGCPercent(300)

	if err := loadEnv(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	logger.Initialize(os.Getenv("DEBUG_MODE") == "true")
	defer logger.Close()

	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		logger.Error("DISCORD_BOT_TOKEN is not set in environment variables")
		os.Exit(1)
	}

	if err := app.Run(token); err != nil {
		logger.Errorf("%v", err)
		os.Exit(1)
	}
}

func loadEnv() error {
	envPath := ".env"

	if _, err := os.Stat(envPath); os.IsNotExist(err) {

		exampleEnv := `# Discord Bot Configuration
DISCORD_BOT_TOKEN=your_bot_token_here

# Optional: Debug mode
DEBUG_MODE=false

# Optional: discordgo library debug logging
DISCORDGO_DEBUG=false
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
