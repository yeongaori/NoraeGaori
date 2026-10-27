package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"noraegaori/internal/app"
	"noraegaori/internal/logger"
)

const (
	exitTokenInvalid = 3
	exitTokenMissing = 4
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
		os.Exit(exitTokenMissing)
	}

	if err := app.Run(token); err != nil {
		if app.IsTokenInvalid(err) {
			logger.Error("DISCORD_BOT_TOKEN is invalid")
			os.Exit(exitTokenInvalid)
		}
		logger.Errorf("%v", err)
		os.Exit(1)
	}
}
