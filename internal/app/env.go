package app

import (
	"fmt"
	"os"

	"noraegaori/internal/logger"

	"github.com/joho/godotenv"
)

func LoadEnv() error {
	envPath := ".env"

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if os.Getenv("DISCORD_BOT_TOKEN") != "" {
			return nil
		}

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
