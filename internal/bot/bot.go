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

func Start(token string) error {
	var err error

	
	session, err = discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("failed to create Discord session: %w", err)
	}

	
	if logger.IsDebug() {
		session.LogLevel = discordgo.LogDebug
	}

	
	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions

	
	session.AddHandler(onReady)
	session.AddHandler(onInteractionCreate)
	session.AddHandler(onMessageCreate)
	session.AddHandler(onVoiceStateUpdate)
	session.AddHandler(onGuildDelete)

	
	if err := session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord connection: %w", err)
	}

	logger.Info("[Bot] Discord connection opened successfully")

	
	waitForShutdown()

	return nil
}

func onReady(s *discordgo.Session, r *discordgo.Ready) {
	logger.Infof("[Bot] Logged in as %s#%s (ID: %s)", r.User.Username, r.User.Discriminator, r.User.ID)
	logger.Infof("[Bot] Connected to %d guilds", len(r.Guilds))

	
	commands.InitializeCommands()

	
	player.SetOnSongStartCallback(func(guildID string) {
		commands.ClearSkipVotes(guildID)
		commands.ClearStopVotes(guildID)
	})

	
	if err := commands.RegisterSlashCommands(s); err != nil {
		logger.Errorf("[Bot] Failed to register slash commands: %v", err)
	}

	
	go rpc.UpdateRPC(s)

	logger.Info("[Bot] Bot is ready and operational")
}

func onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	commands.HandleInteraction(s, i)
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	commands.HandleMessage(s, m)
}

func onVoiceStateUpdate(s *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	player.HandleVoiceStateUpdate(s, vsu)
}

func onGuildDelete(s *discordgo.Session, g *discordgo.GuildDelete) {
	logger.Infof("[Bot] Bot removed from guild: %s - cleaning up data", g.ID)

	
	if err := player.Stop(g.ID); err != nil {
		logger.Debugf("[Bot] Failed to stop player for guild %s: %v", g.ID, err)
	}

	
	if err := queue.DeleteGuildData(g.ID); err != nil {
		logger.Errorf("[Bot] Failed to delete guild data for %s: %v", g.ID, err)
	} else {
		logger.Infof("[Bot] Successfully cleaned up all data for guild: %s", g.ID)
	}
}

func waitForShutdown() {
	logger.Info("[Bot] Bot is running. Press Ctrl+C to stop")

	
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	
	<-sc

	
	shutdown.SetShuttingDown()

	logger.Info("[Bot] Received shutdown signal, cleaning up...")

	
	logger.Info("[Bot] Stopping RPC updates...")
	rpc.Stop()

	
	logger.Info("[Bot] Stopping all active players...")
	player.StopAll()

	
	logger.Info("[Bot] Shutting down worker pool...")
	player.ShutdownWorkerPool()

	
	if session != nil {
		logger.Info("[Bot] Closing Discord session...")
		if err := session.Close(); err != nil {
			logger.Errorf("[Bot] Error closing Discord session: %v", err)
		}
	}

	logger.Info("[Bot] Shutdown complete")
}

func GetSession() *discordgo.Session {
	return session
}
