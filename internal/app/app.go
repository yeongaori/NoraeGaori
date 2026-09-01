package app

import (
	"context"
	"fmt"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/opus"
	"noraegaori/internal/config"
	"noraegaori/internal/database"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/youtube"
	ytdlpUpdater "noraegaori/internal/ytdlp"
)

func Run(token string) error {
	logger.Debug("Initializing database...")
	if err := database.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Errorf("Failed to close database: %v", err)
		}
	}()

	logger.Debug("Clearing stale playback states...")
	if err := queue.ClearStalePlaybackStates(); err != nil {
		logger.Warnf("Failed to clear stale states: %v", err)
	}

	if err := analysis.PruneTrackAnalysis(); err != nil {
		logger.Warnf("Failed to prune stored track analysis: %v", err)
	}

	logger.Debug("Loading configuration...")
	if err := config.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	defer func() {
		if err := config.Close(); err != nil {
			logger.Errorf("Failed to close config watcher: %v", err)
		}
	}()

	logger.SetLogFile(config.GetConfig().LogFile)
	config.OnReload(func() {
		logger.SetLogFile(config.GetConfig().LogFile)
	})

	messages.SetGuildLangResolver(guild.GetLanguage)

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
		command.ReloadAliases()
	})

	logger.Debug("Initializing yt-dlp version manager...")
	if err := ytdlpUpdater.InitVersionManager(); err != nil {
		logger.Warnf("Failed to initialize yt-dlp version manager: %v", err)
	}

	logger.Debug("Initializing YouTube integration...")
	if err := youtube.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize YouTube: %w", err)
	}

	ytdlpUpdater.AutoUpdate()
	ytdlpUpdater.DetectJsRuntime()

	updaterCtx, updaterCancel := context.WithCancel(context.Background())
	defer updaterCancel()
	ytdlpUpdater.StartBackgroundUpdater(updaterCtx)

	logger.Debugf("Opus encoder: %s", opus.GetEncoderType())
	logger.Info("Starting Discord bot...")
	if err := Start(token); err != nil {
		return fmt.Errorf("failed to start bot: %w", err)
	}

	logger.Debug("Bot stopped successfully")
	return nil
}
