package bot

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/rpc"
	"noraegaori/internal/shutdown"
	"noraegaori/pkg/logger"
)

var (
	session *discordgo.Session
)

// Start initializes and starts the Discord bot
func Start(token string) error {
	var err error

	// Create Discord session
	session, err = discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("failed to create Discord session: %w", err)
	}

	// Enable discordgo debug logging if debug mode is on
	if logger.IsDebug() {
		session.LogLevel = discordgo.LogDebug
	}

	// Set intents
	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions

	// Register event handlers
	session.AddHandler(onReady)
	session.AddHandler(onInteractionCreate)
	session.AddHandler(onMessageCreate)
	session.AddHandler(onVoiceStateUpdate)
	session.AddHandler(onGuildDelete)

	// Open connection
	if err := session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord connection: %w", err)
	}

	logger.Info("[Bot] Discord connection opened successfully")

	// Wait for interrupt signal
	waitForShutdown()

	return nil
}

// onReady is called when the bot is ready
func onReady(s *discordgo.Session, r *discordgo.Ready) {
	logger.Infof("[Bot] Logged in as %s#%s (ID: %s)", r.User.Username, r.User.Discriminator, r.User.ID)
	logger.Infof("[Bot] Connected to %d guilds", len(r.Guilds))

	// Initialize commands
	commands.InitializeCommands()

	// Register player callbacks
	player.SetOnSongStartCallback(func(guildID string) {
		commands.ClearSkipVotes(guildID)
		commands.ClearStopVotes(guildID)
	})

	// Register slash commands
	if err := commands.RegisterSlashCommands(s); err != nil {
		logger.Errorf("[Bot] Failed to register slash commands: %v", err)
	}

	// Start RPC handler
	go rpc.UpdateRPC(s)

	logger.Info("[Bot] Bot is ready and operational")
}

// onInteractionCreate handles slash command interactions
func onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	commands.HandleInteraction(s, i)
}

// onMessageCreate handles message events
func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	commands.HandleMessage(s, m)
}

// onVoiceStateUpdate handles voice state updates
func onVoiceStateUpdate(s *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	player.HandleVoiceStateUpdate(s, vsu)
}

// onGuildDelete handles guild deletion (bot removed from server)
func onGuildDelete(s *discordgo.Session, g *discordgo.GuildDelete) {
	logger.Infof("[Bot] Bot removed from guild: %s - cleaning up data", g.ID)

	// Stop any active player for this guild
	if err := player.Stop(g.ID); err != nil {
		logger.Debugf("[Bot] Failed to stop player for guild %s: %v", g.ID, err)
	}

	// Delete all guild data (queue, songs, settings)
	if err := queue.DeleteGuildData(g.ID); err != nil {
		logger.Errorf("[Bot] Failed to delete guild data for %s: %v", g.ID, err)
	} else {
		logger.Infof("[Bot] Successfully cleaned up all data for guild: %s", g.ID)
	}
}

// waitForShutdown waits for an interrupt signal and performs graceful shutdown
func waitForShutdown() {
	logger.Info("[Bot] Bot is running. Press Ctrl+C to stop")

	// Create signal channel
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	// Wait for signal
	<-sc

	// Set shutdown flag immediately to prevent new operations
	shutdown.SetShuttingDown()

	logger.Info("[Bot] Received shutdown signal, cleaning up...")

	// Stop RPC updates
	logger.Info("[Bot] Stopping RPC updates...")
	rpc.Stop()

	// Stop all active players
	logger.Info("[Bot] Stopping all active players...")
	player.StopAll()

	// Close worker pool
	logger.Info("[Bot] Shutting down worker pool...")
	player.ShutdownWorkerPool()

	// Close Discord session
	if session != nil {
		logger.Info("[Bot] Closing Discord session...")
		if err := session.Close(); err != nil {
			logger.Errorf("[Bot] Error closing Discord session: %v", err)
		}
	}

	logger.Info("[Bot] Shutdown complete")
}

// GetSession returns the Discord session
func GetSession() *discordgo.Session {
	return session
}
