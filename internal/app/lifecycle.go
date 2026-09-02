package app

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"noraegaori/internal/logger"
	"noraegaori/internal/player"
	"noraegaori/internal/rpc"
	"noraegaori/internal/shutdown"
)

const forcedExitTimeout = 15 * time.Second

func forceExitAfter(timeout time.Duration, exit func(int)) {
	time.AfterFunc(timeout, func() {
		logger.Warnf("Shutdown timed out after %s, forcing exit", timeout)
		exit(1)
	})
}

func waitForShutdown() {
	logger.Info("Bot is running. Press Ctrl+C to stop")

	sc := make(chan os.Signal, 2)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	<-sc

	shutdown.SetShuttingDown()

	logger.Info("Received shutdown signal, cleaning up... (press Ctrl+C again to force quit)")

	forceExitAfter(forcedExitTimeout, os.Exit)

	go func() {
		<-sc
		logger.Warn("Second shutdown signal received, forcing exit")
		os.Exit(1)
	}()

	logger.Debug("Stopping RPC updates...")
	rpc.Stop()

	logger.Debug("Stopping all active players...")
	player.StopAll()

	logger.Debug("Shutting down worker pool...")
	player.ShutdownWorkerPool()

	if session != nil {
		logger.Debug("Closing Discord session...")
		if err := session.Close(); err != nil {
			logger.Errorf("Error closing Discord session: %v", err)
		}
	}

	logger.Info("Shutdown complete")
}
