package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"noraegaori/internal/app"
	"noraegaori/internal/logger"
)

func main() {
	debug.SetGCPercent(300)

	if err := app.LoadEnv(); err != nil {
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
