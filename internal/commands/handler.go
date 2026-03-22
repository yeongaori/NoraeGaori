package commands

import (
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

// Command represents a bot command
type Command struct {
	Name        string
	Description string
	Options     []*discordgo.ApplicationCommandOption
	Handler     func(s *discordgo.Session, i *discordgo.InteractionCreate) error
	AdminOnly   bool
	TextOnly    bool   // If true, command is only available via text (not registered as slash command)
	Usage       string // Usage string for help display (e.g., "play <title|URL>")
	Example     string // Example usage for help display (e.g., "play IU Bam Pyeonji")
}

var (
	commands          = make(map[string]*Command)
	aliases           = make(map[string]string) // alias -> command name
	messageResponders sync.Map                  // token -> *MessageResponse (for message-based commands)
)

// isGuildAdmin checks if a member has Administrator permission in the guild
func isGuildAdmin(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
	if member == nil {
		return false
	}

	// Get guild from state or API
	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			logger.Debugf("[Permissions] Failed to get guild %s: %v", guildID, err)
			return false
		}
	}

	// Calculate member permissions
	var perms int64 = 0

	// Get @everyone role permissions
	for _, role := range guild.Roles {
		if role.ID == guildID { // @everyone role has same ID as guild
			perms |= role.Permissions
			break
		}
	}

	// Apply role permissions
	for _, roleID := range member.Roles {
		for _, role := range guild.Roles {
			if role.ID == roleID {
				perms |= role.Permissions
				break
			}
		}
	}

	// Check for Administrator permission (0x8)
	return (perms & discordgo.PermissionAdministrator) == discordgo.PermissionAdministrator
}

// RegisterCommand registers a command
func RegisterCommand(cmd *Command) {
	commands[cmd.Name] = cmd
	logger.Debugf("[Commands] Registered command: %s", cmd.Name)
}

// RegisterAlias registers a command alias
func RegisterAlias(alias, commandName string) {
	aliases[alias] = commandName
	logger.Debugf("[Commands] Registered alias: %s -> %s", alias, commandName)
}

// registerCommandAliases registers all aliases for a command from the locale
func registerCommandAliases(name string, cs messages.CommandStrings) {
	for _, alias := range cs.Aliases {
		RegisterAlias(alias, name)
	}
}

// ReloadAliases clears and re-registers all command aliases and descriptions
// from the current locale. Called when the language changes at runtime.
func ReloadAliases() {
	// Clear existing aliases
	aliases = make(map[string]string)

	t := messages.T()
	cmd := func(name string) messages.CommandStrings {
		if t != nil {
			if c, ok := t.Commands[name]; ok {
				return c
			}
		}
		return messages.CommandStrings{}
	}

	// Re-register aliases and update descriptions from the new locale
	for name, c := range commands {
		cs := cmd(name)
		registerCommandAliases(name, cs)
		if cs.Description != "" {
			c.Description = cs.Description
		}
		if cs.Usage != "" {
			c.Usage = cs.Usage
		}
		if cs.Example != "" {
			c.Example = cs.Example
		}
	}

	logger.Info("[Commands] Aliases and descriptions reloaded for new locale")
}

// InitializeCommands registers all commands
func InitializeCommands() {
	t := messages.T()
	cmd := func(name string) messages.CommandStrings {
		if t != nil {
			if c, ok := t.Commands[name]; ok {
				return c
			}
		}
		return messages.CommandStrings{}
	}

	// Music playback commands
	RegisterCommand(&Command{
		Name:        "play",
		Description: cmd("play").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: cmd("play").Options["query"],
				Required:    true,
			},
		},
		Handler:  HandlePlay,
		TextOnly: false,
		Usage:    cmd("play").Usage,
		Example:  cmd("play").Example,
	})
	registerCommandAliases("play", cmd("play"))

	RegisterCommand(&Command{
		Name:        "playnext",
		Description: cmd("playnext").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: cmd("playnext").Options["query"],
				Required:    true,
			},
		},
		Handler:  HandlePlayNext,
		TextOnly: false,
		Usage:    cmd("playnext").Usage,
		Example:  cmd("playnext").Example,
	})
	registerCommandAliases("playnext", cmd("playnext"))

	RegisterCommand(&Command{
		Name:        "search",
		Description: cmd("search").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "query",
				Description: cmd("search").Options["query"],
				Required:    true,
			},
		},
		Handler:  HandleSearch,
		TextOnly: false,
		Usage:    cmd("search").Usage,
		Example:  cmd("search").Example,
	})
	registerCommandAliases("search", cmd("search"))

	RegisterCommand(&Command{
		Name:        "pause",
		Description: cmd("pause").Description,
		Handler:     HandlePause,
		TextOnly:    false,
		Usage:       cmd("pause").Usage,
		Example:     cmd("pause").Example,
	})
	registerCommandAliases("pause", cmd("pause"))

	RegisterCommand(&Command{
		Name:        "resume",
		Description: cmd("resume").Description,
		Handler:     HandleResume,
		TextOnly:    false,
		Usage:       cmd("resume").Usage,
		Example:     cmd("resume").Example,
	})
	registerCommandAliases("resume", cmd("resume"))

	RegisterCommand(&Command{
		Name:        "skip",
		Description: cmd("skip").Description,
		Handler:     HandleSkip,
		TextOnly:    false,
		Usage:       cmd("skip").Usage,
		Example:     cmd("skip").Example,
	})

	RegisterCommand(&Command{
		Name:        "stop",
		Description: cmd("stop").Description,
		Handler:     HandleStop,
		TextOnly:    false,
		Usage:       cmd("stop").Usage,
		Example:     cmd("stop").Example,
	})
	registerCommandAliases("stop", cmd("stop"))

	RegisterCommand(&Command{
		Name:        "nowplaying",
		Description: cmd("nowplaying").Description,
		Handler:     HandleNowPlaying,
		TextOnly:    false,
		Usage:       cmd("nowplaying").Usage,
		Example:     cmd("nowplaying").Example,
	})
	registerCommandAliases("nowplaying", cmd("nowplaying"))

	RegisterCommand(&Command{
		Name:        "volume",
		Description: cmd("volume").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionNumber,
				Name:        "level",
				Description: cmd("volume").Options["level"],
				Required:    false,
				MinValue:    func() *float64 { v := 0.0; return &v }(),
				MaxValue:    1000.0,
			},
		},
		Handler:  HandleVolume,
		TextOnly: false,
		Usage:    cmd("volume").Usage,
		Example:  cmd("volume").Example,
	})
	registerCommandAliases("volume", cmd("volume"))

	RegisterCommand(&Command{
		Name:        "repeat",
		Description: cmd("repeat").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "mode",
				Description: cmd("repeat").Options["mode"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "single", Value: "single"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleRepeat,
		TextOnly: false,
		Usage:    cmd("repeat").Usage,
		Example:  cmd("repeat").Example,
	})
	registerCommandAliases("repeat", cmd("repeat"))

	// Queue management commands
	RegisterCommand(&Command{
		Name:        "queue",
		Description: cmd("queue").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("queue").Options["page"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleQueue,
		TextOnly: false,
		Usage:    cmd("queue").Usage,
		Example:  cmd("queue").Example,
	})
	registerCommandAliases("queue", cmd("queue"))

	RegisterCommand(&Command{
		Name:        "remove",
		Description: cmd("remove").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "position",
				Description: cmd("remove").Options["position"],
				Required:    true,
			},
		},
		Handler:  HandleRemove,
		TextOnly: false,
		Usage:    cmd("remove").Usage,
		Example:  cmd("remove").Example,
	})
	registerCommandAliases("remove", cmd("remove"))

	RegisterCommand(&Command{
		Name:        "swap",
		Description: cmd("swap").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "position1",
				Description: cmd("swap").Options["position1"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "position2",
				Description: cmd("swap").Options["position2"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleSwap,
		TextOnly: false,
		Usage:    cmd("swap").Usage,
		Example:  cmd("swap").Example,
	})

	RegisterCommand(&Command{
		Name:        "skipto",
		Description: cmd("skipto").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "position",
				Description: cmd("skipto").Options["position"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleSkipTo,
		TextOnly: false,
		Usage:    cmd("skipto").Usage,
		Example:  cmd("skipto").Example,
	})
	registerCommandAliases("skipto", cmd("skipto"))

	// Voice channel commands
	RegisterCommand(&Command{
		Name:        "join",
		Description: cmd("join").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "channel",
				Description: cmd("join").Options["channel"],
				Required:    false,
				ChannelTypes: []discordgo.ChannelType{
					discordgo.ChannelTypeGuildVoice,
				},
			},
		},
		Handler:  HandleJoin,
		TextOnly: false,
		Usage:    cmd("join").Usage,
		Example:  cmd("join").Example,
	})
	registerCommandAliases("join", cmd("join"))

	RegisterCommand(&Command{
		Name:        "leave",
		Description: cmd("leave").Description,
		Handler:     HandleLeave,
		TextOnly:    false,
		Usage:       cmd("leave").Usage,
		Example:     cmd("leave").Example,
	})
	registerCommandAliases("leave", cmd("leave"))

	RegisterCommand(&Command{
		Name:        "switchvc",
		Description: cmd("switchvc").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "channel",
				Description: cmd("switchvc").Options["channel"],
				Required:    false,
				ChannelTypes: []discordgo.ChannelType{
					discordgo.ChannelTypeGuildVoice,
				},
			},
		},
		Handler:  HandleSwitchVC,
		TextOnly: false,
		Usage:    cmd("switchvc").Usage,
		Example:  cmd("switchvc").Example,
	})
	registerCommandAliases("switchvc", cmd("switchvc"))

	// Settings commands
	RegisterCommand(&Command{
		Name:        "sponsorblock",
		Description: cmd("sponsorblock").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("sponsorblock").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleSponsorBlock,
		TextOnly: false,
		Usage:    cmd("sponsorblock").Usage,
		Example:  cmd("sponsorblock").Example,
	})
	registerCommandAliases("sponsorblock", cmd("sponsorblock"))

	RegisterCommand(&Command{
		Name:        "showstartedtrack",
		Description: cmd("showstartedtrack").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("showstartedtrack").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleShowStartedTrack,
		TextOnly: false,
		Usage:    cmd("showstartedtrack").Usage,
		Example:  cmd("showstartedtrack").Example,
	})
	registerCommandAliases("showstartedtrack", cmd("showstartedtrack"))

	RegisterCommand(&Command{
		Name:        "normalization",
		Description: cmd("normalization").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("normalization").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleNormalization,
		TextOnly: false,
		Usage:    cmd("normalization").Usage,
		Example:  cmd("normalization").Example,
	})
	registerCommandAliases("normalization", cmd("normalization"))

	RegisterCommand(&Command{
		Name:        "setprefix",
		Description: cmd("setprefix").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "prefix",
				Description: cmd("setprefix").Options["prefix"],
				Required:    false,
			},
		},
		Handler:   HandleSetPrefix,
		AdminOnly: true,
		TextOnly:  true,
		Usage:     cmd("setprefix").Usage,
		Example:   cmd("setprefix").Example,
	})
	registerCommandAliases("setprefix", cmd("setprefix"))

	// Admin commands (not registered as slash commands in Node.js)
	RegisterCommand(&Command{
		Name:        "forceskip",
		Description: cmd("forceskip").Description,
		Handler:     HandleForceSkip,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("forceskip").Usage,
		Example:     cmd("forceskip").Example,
	})
	registerCommandAliases("forceskip", cmd("forceskip"))

	RegisterCommand(&Command{
		Name:        "forceremove",
		Description: cmd("forceremove").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "target",
				Description: cmd("forceremove").Options["target"],
				Required:    true,
			},
		},
		Handler:   HandleForceRemove,
		AdminOnly: true,
		TextOnly:  true,
		Usage:     cmd("forceremove").Usage,
		Example:   cmd("forceremove").Example,
	})
	registerCommandAliases("forceremove", cmd("forceremove"))

	RegisterCommand(&Command{
		Name:        "forcestop",
		Description: cmd("forcestop").Description,
		Handler:     HandleForceStop,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("forcestop").Usage,
		Example:     cmd("forcestop").Example,
	})
	registerCommandAliases("forcestop", cmd("forcestop"))

	RegisterCommand(&Command{
		Name:        "movetrack",
		Description: cmd("movetrack").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "from",
				Description: cmd("movetrack").Options["from"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "to",
				Description: cmd("movetrack").Options["to"],
				Required:    true,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:   HandleMoveTrack,
		AdminOnly: true,
		TextOnly:  true,
		Usage:     cmd("movetrack").Usage,
		Example:   cmd("movetrack").Example,
	})
	registerCommandAliases("movetrack", cmd("movetrack"))

	RegisterCommand(&Command{
		Name:        "status",
		Description: cmd("status").Description,
		Handler:     HandleStatus,
		AdminOnly:   true,
		TextOnly:    true,
		Usage:       cmd("status").Usage,
		Example:     cmd("status").Example,
	})

	// Help command
	RegisterCommand(&Command{
		Name:        "help",
		Description: cmd("help").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "page",
				Description: cmd("help").Options["page"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
			},
		},
		Handler:  HandleHelp,
		TextOnly: false,
		Usage:    cmd("help").Usage,
		Example:  cmd("help").Example,
	})
	registerCommandAliases("help", cmd("help"))

	logger.Info("[Commands] All commands registered")
}

// RegisterSlashCommands registers slash commands with Discord
func RegisterSlashCommands(session *discordgo.Session) error {
	logger.Info("[Commands] Registering slash commands with Discord...")

	// Get existing commands
	existing, err := session.ApplicationCommands(session.State.User.ID, "")
	if err != nil {
		return fmt.Errorf("failed to get existing commands: %w", err)
	}

	// Create a map of existing commands
	existingMap := make(map[string]*discordgo.ApplicationCommand)
	for _, cmd := range existing {
		existingMap[cmd.Name] = cmd
	}

	// Register new commands
	for name, cmd := range commands {
		// Skip text-only commands from slash registration
		if cmd.TextOnly {
			logger.Debugf("[Commands] Skipping text-only command from slash registration: %s", name)
			continue
		}

		appCmd := &discordgo.ApplicationCommand{
			Name:        cmd.Name,
			Description: cmd.Description,
			Options:     cmd.Options,
		}

		// Check if command already exists
		if existing, ok := existingMap[name]; ok {
			// Update if changed
			if existing.Description != cmd.Description {
				_, err := session.ApplicationCommandEdit(session.State.User.ID, "", existing.ID, appCmd)
				if err != nil {
					logger.Errorf("[Commands] Failed to update command %s: %v", name, err)
				} else {
					logger.Infof("[Commands] Updated command: %s", name)
				}
			}
			delete(existingMap, name)
		} else {
			// Create new command
			_, err := session.ApplicationCommandCreate(session.State.User.ID, "", appCmd)
			if err != nil {
				logger.Errorf("[Commands] Failed to create command %s: %v", name, err)
			} else {
				logger.Infof("[Commands] Created command: %s", name)
			}
		}
	}

	// Remove old commands that no longer exist
	for name, cmd := range existingMap {
		if err := session.ApplicationCommandDelete(session.State.User.ID, "", cmd.ID); err != nil {
			logger.Errorf("[Commands] Failed to delete old command %s: %v", name, err)
		} else {
			logger.Infof("[Commands] Deleted old command: %s", name)
		}
	}

	logger.Info("[Commands] Slash commands registered successfully")
	return nil
}

// HandleInteraction handles slash command interactions
func HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	cmdName := i.ApplicationCommandData().Name
	cmd, exists := commands[cmdName]
	if !exists {
		logger.Warnf("[Commands] Unknown command: %s", cmdName)
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, messages.T().Errors.UnknownCommand))
		return
	}

	// Check admin permission (bot admin or server admin)
	if cmd.AdminOnly {
		if !config.IsAdmin(i.Member.User.ID) && !isGuildAdmin(s, i.GuildID, i.Member) {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleNoPermission, messages.ErrorAdminOnly))
			return
		}
	}

	logger.Infof("[Commands] Executing command: %s (user: %s, guild: %s)",
		cmdName, i.Member.User.Username, i.GuildID)

	// Execute command
	if err := cmd.Handler(s, i); err != nil {
		logger.Errorf("[Commands] Command %s failed: %v", cmdName, err)
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Errors.CommandExecutionError, err)))
	}
}

// HandleMessage handles text-based commands
func HandleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore bots
	if m.Author.Bot {
		return
	}

	cfg := config.GetConfig()
	prefix := cfg.Prefix

	// Check if message starts with prefix
	if !strings.HasPrefix(m.Content, prefix) {
		return
	}

	// Parse command and args
	content := strings.TrimPrefix(m.Content, prefix)
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	_ = parts[1:] // args, unused in text commands

	// Resolve command through aliases only — all user-typeable names
	// (including the English command name) come from the locale file.
	aliasTarget, ok := aliases[cmdName]
	if !ok {
		return // Silently ignore unknown commands
	}
	cmdName = aliasTarget

	// Find command
	cmd, exists := commands[cmdName]
	if !exists {
		return // Silently ignore unknown commands
	}

	// Check admin permission (bot admin or server admin)
	if cmd.AdminOnly {
		// Get member to check server permissions
		member, err := s.State.Member(m.GuildID, m.Author.ID)
		if err != nil {
			// Try to fetch from API if not in state
			member, err = s.GuildMember(m.GuildID, m.Author.ID)
		}

		isBotAdmin := config.IsAdmin(m.Author.ID)
		isServerAdmin := (err == nil) && isGuildAdmin(s, m.GuildID, member)

		if !isBotAdmin && !isServerAdmin {
			embed := messages.CreateErrorEmbed(messages.TitleNoPermission, messages.ErrorAdminOnly)
			// Send error as reply to original message
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{embed},
				Reference: &discordgo.MessageReference{
					MessageID: m.ID,
					ChannelID: m.ChannelID,
				},
			})
			return
		}
	}

	logger.Infof("[Commands] Executing text command: %s (user: %s, guild: %s)",
		cmdName, m.Author.Username, m.GuildID)

	// Parse args (everything after command name)
	args := parts[1:]

	// Create pseudo-interaction for text commands
	pseudoInteraction := CreatePseudoInteraction(s, m, cmd, args)

	// Store message context for responses
	messageResponder := &MessageResponse{
		Session:       s,
		ChannelID:     m.ChannelID,
		Message:       nil, // Will be set to bot's response when SendEmbed is called
		OriginalMsgID: m.ID, // Store original message ID for reply
	}

	// Store in a map for response handlers to access
	messageResponders.Store(pseudoInteraction.Token, messageResponder)
	defer messageResponders.Delete(pseudoInteraction.Token)

	// Execute command handler
	if err := cmd.Handler(s, pseudoInteraction); err != nil {
		logger.Errorf("[Commands] Text command %s failed: %v", cmdName, err)
		// Only send error message if no message was already sent by the command handler
		if messageResponder.Message == nil {
			embed := messages.CreateErrorEmbed(messages.TitleError, fmt.Sprintf(messages.T().Errors.CommandExecutionError, err))
			// Send error as reply to original message
			s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{embed},
				Reference: &discordgo.MessageReference{
					MessageID: m.ID,
					ChannelID: m.ChannelID,
				},
			})
		}
	}
}

// isMessageCommand checks if the interaction is from a message command
func isMessageCommand(i *discordgo.InteractionCreate) bool {
	return strings.HasPrefix(i.Token, "message_")
}

// RespondError sends an error response
func RespondError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendMessage(message)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// RespondSuccess sends a success response
func RespondSuccess(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendMessage(message)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})
}

// RespondEmbed sends an embed response
func RespondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendEmbed(embed)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// RespondEmbedWithComponents sends an embed response with components (buttons, select menus, etc.)
func RespondEmbedWithComponents(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			return mr.(*MessageResponse).SendEmbedWithComponents(embed, components)
		}
		return nil, fmt.Errorf("message responder not found")
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		return nil, err
	}

	// Get the response message
	return s.InteractionResponse(i.Interaction)
}

// DeferResponse defers the response (for long-running commands)
func DeferResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if isMessageCommand(i) {
		// For message commands, send a loading embed that can be edited later
		if mr, ok := messageResponders.Load(i.Token); ok {
			loadingEmbed := &discordgo.MessageEmbed{
				Color:       0xFFA500, // Orange color
				Title:       messages.TitleLoading,
				Description: messages.DescLoading,
			}
			mr.(*MessageResponse).SendEmbed(loadingEmbed)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
}

// FollowUpMessage sends a follow-up message
func FollowUpMessage(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendFollowUp(content)
		}
		return
	}
	s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Content: content,
	})
}

// FollowUpEmbed sends a follow-up embed
func FollowUpEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			mr.(*MessageResponse).SendFollowUpEmbed(embed)
		}
		return
	}
	s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

// UpdateResponseEmbed updates an existing response message (for message commands) or interaction response (for slash commands)
func UpdateResponseEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	if isMessageCommand(i) {
		// For message commands, edit the last sent message
		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				_, err := s.ChannelMessageEditEmbed(responder.ChannelID, responder.Message.ID, embed)
				return err
			}
		}
		return fmt.Errorf("message responder or message not found")
	}
	// For slash commands, use InteractionResponseEdit
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
	return err
}

// UpdateResponseEmbedWithComponents updates an existing response with embed and components
func UpdateResponseEmbedWithComponents(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	if isMessageCommand(i) {
		// For message commands, edit the last sent message
		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				logger.Debugf("[UpdateResponse] Editing message %s in channel %s", responder.Message.ID, responder.ChannelID)
				_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:    responder.ChannelID,
					ID:         responder.Message.ID,
					Embeds:     &[]*discordgo.MessageEmbed{embed},
					Components: &components,
				})
				if err != nil {
					logger.Errorf("[UpdateResponse] Failed to edit message: %v", err)
				}
				return err
			}
			logger.Errorf("[UpdateResponse] Message is nil in responder")
			return fmt.Errorf("message is nil")
		}
		logger.Errorf("[UpdateResponse] Message responder not found for token: %s", i.Token)
		return fmt.Errorf("message responder not found")
	}
	// For slash commands, use InteractionResponseEdit
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
	return err
}

// GetResponseMessage gets the response message for an interaction
// For text commands: returns the stored Message from MessageResponse
// For slash commands: calls s.InteractionResponse to get the message
func GetResponseMessage(s *discordgo.Session, i *discordgo.InteractionCreate) (*discordgo.Message, error) {
	if isMessageCommand(i) {
		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				return responder.Message, nil
			}
			return nil, fmt.Errorf("message not found in responder")
		}
		return nil, fmt.Errorf("message responder not found")
	}
	return s.InteractionResponse(i.Interaction)
}

// checkUserInBotVoiceChannel verifies that the user is in the same voice channel as the bot
// Returns the voice channel ID if successful, or an error embed if not
func checkUserInBotVoiceChannel(s *discordgo.Session, i *discordgo.InteractionCreate) (string, *discordgo.MessageEmbed) {
	// Check if user is in a voice channel
	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		return "", messages.CreateErrorEmbed(messages.TitleError, messages.ErrorNotInVoiceChannel)
	}

	// Get the queue to find bot's current voice channel
	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || q.VoiceChannelID == "" {
		// No queue or no voice channel set - bot is not in a channel
		return voiceState.ChannelID, nil
	}

	// Check if user is in the same voice channel as the bot
	if voiceState.ChannelID != q.VoiceChannelID {
		return "", messages.CreateErrorEmbed(messages.TitleError, messages.T().Errors.MustBeInBotChannel)
	}

	return voiceState.ChannelID, nil
}
