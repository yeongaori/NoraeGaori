package app

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/logger"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/internal/rpc"
	"noraegaori/internal/vote"
)

var session *discordgo.Session

func Start(token string) error {
	var err error

	session, err = discordgo.New("Bot " + token)
	if err != nil {
		return fmt.Errorf("failed to create Discord session: %w", err)
	}

	if os.Getenv("DISCORDGO_DEBUG") == "true" {
		session.LogLevel = discordgo.LogDebug
		discordgo.Logger = redactToken(token)
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions

	if presence, ok := rpc.LoadIdentifyPresence(); ok {
		session.Identify.Presence = presence
	}

	session.AddHandler(onReady)
	session.AddHandler(onInteractionCreate)
	session.AddHandler(onMessageCreate)
	session.AddHandler(onVoiceStateUpdate)
	session.AddHandler(onGuildDelete)

	if err := session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord connection: %w", err)
	}

	logger.Debug("Discord connection opened successfully")

	waitForShutdown()

	return nil
}

func redactToken(token string) func(msgL, caller int, format string, a ...interface{}) {
	return func(msgL, caller int, format string, a ...interface{}) {
		pc, file, line, _ := runtime.Caller(caller + 1)
		file = file[strings.LastIndex(file, "/")+1:]
		name := runtime.FuncForPC(pc).Name()
		name = name[strings.LastIndex(name, ".")+1:]

		message := strings.ReplaceAll(fmt.Sprintf(format, a...), token, "[token]")
		log.Printf("[DG%d] %s:%d:%s() %s\n", msgL, file, line, name, message)
	}
}

func onReady(s *discordgo.Session, r *discordgo.Ready) {
	logger.Infof("Logged in as %s#%s (ID: %s)", r.User.Username, r.User.Discriminator, r.User.ID)
	logger.Infof("Connected to %d guilds", len(r.Guilds))

	commands.InitializeCommands()

	player.SetOnSongStartCallback(vote.CancelSkipVotes)
	player.SetOnPlaybackEndedCallback(vote.CancelForEndedPlayback)
	command.SetBeforeCommand(player.ReconcileState)

	vote.RegisterDispatcher(s)

	if err := command.RegisterSlashCommands(s); err != nil {
		logger.Errorf("Failed to register slash commands: %v", err)
	}

	go rpc.UpdateRPC(s)

	logger.Info("Bot is ready and operational")
}

func onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	command.HandleInteraction(s, i)
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	command.HandleMessage(s, m)
}

func onVoiceStateUpdate(s *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	player.HandleVoiceStateUpdate(s, vsu)
}

func onGuildDelete(s *discordgo.Session, g *discordgo.GuildDelete) {
	logger.Infof("Bot removed from guild: %s - cleaning up data", g.ID)

	if err := player.Teardown(g.ID); err != nil {
		logger.Debugf("Failed to stop player for guild %s: %v", g.ID, err)
	}

	if err := queue.DeleteGuildData(g.ID); err != nil {
		logger.Errorf("Failed to delete guild data for %s: %v", g.ID, err)
	} else {
		logger.Infof("Successfully cleaned up all data for guild: %s", g.ID)
	}
}

func GetSession() *discordgo.Session {
	return session
}
