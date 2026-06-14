package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"

	"github.com/bwmarrin/discordgo"
)

type Command struct {
	Name        string
	Description string
	Options     []*discordgo.ApplicationCommandOption
	Handler     func(s *discordgo.Session, i *discordgo.InteractionCreate) error
	AdminOnly   bool
	TextOnly    bool
	Usage       string
	Example     string
}

var (
	commands          = make(map[string]*Command)
	aliases           = make(map[string]string)
	messageResponders sync.Map
)

func isGuildAdmin(s *discordgo.Session, guildID string, member *discordgo.Member) bool {
	if member == nil {
		return false
	}

	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			logger.Debugf("[Permissions] Failed to get guild %s: %v", guildID, err)
			return false
		}
	}

	var perms int64 = 0

	for _, role := range guild.Roles {
		if role.ID == guildID {
			perms |= role.Permissions
			break
		}
	}

	for _, roleID := range member.Roles {
		for _, role := range guild.Roles {
			if role.ID == roleID {
				perms |= role.Permissions
				break
			}
		}
	}

	return (perms & discordgo.PermissionAdministrator) == discordgo.PermissionAdministrator
}

func RegisterCommand(cmd *Command) {
	commands[cmd.Name] = cmd
	logger.Debugf("[Commands] Registered command: %s", cmd.Name)
}

func RegisterAlias(alias, commandName string) {
	aliases[alias] = commandName
	logger.Debugf("[Commands] Registered alias: %s -> %s", alias, commandName)
}

func registerCommandAliases(name string, cs messages.CommandStrings) {
	for _, alias := range cs.Aliases {
		RegisterAlias(alias, name)
	}
}

func ReloadAliases() {

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
		Name:        "seek",
		Description: cmd("seek").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "position",
				Description: cmd("seek").Options["position"],
				Required:    true,
			},
		},
		Handler:  HandleSeek,
		TextOnly: false,
		Usage:    cmd("seek").Usage,
		Example:  cmd("seek").Example,
	})
	registerCommandAliases("seek", cmd("seek"))

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
		Name:        "fadein",
		Description: cmd("fadein").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("fadein").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: cmd("fadein").Options["duration"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
				MaxValue:    30.0,
			},
		},
		Handler:  HandleFadeIn,
		TextOnly: false,
		Usage:    cmd("fadein").Usage,
		Example:  cmd("fadein").Example,
	})
	registerCommandAliases("fadein", cmd("fadein"))

	RegisterCommand(&Command{
		Name:        "fadeout",
		Description: cmd("fadeout").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("fadeout").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: cmd("fadeout").Options["duration"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
				MaxValue:    30.0,
			},
		},
		Handler:  HandleFadeOut,
		TextOnly: false,
		Usage:    cmd("fadeout").Usage,
		Example:  cmd("fadeout").Example,
	})
	registerCommandAliases("fadeout", cmd("fadeout"))

	RegisterCommand(&Command{
		Name:        "automix",
		Description: cmd("automix").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("automix").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "beats",
				Description: cmd("automix").Options["beats"],
				Required:    false,
				MinValue:    func() *float64 { v := 4.0; return &v }(),
				MaxValue:    64.0,
			},
		},
		Handler:  HandleAutoMix,
		TextOnly: false,
		Usage:    cmd("automix").Usage,
		Example:  cmd("automix").Example,
	})
	registerCommandAliases("automix", cmd("automix"))

	RegisterCommand(&Command{
		Name:        "crossfade",
		Description: cmd("crossfade").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("crossfade").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "duration",
				Description: cmd("crossfade").Options["duration"],
				Required:    false,
				MinValue:    func() *float64 { v := 1.0; return &v }(),
				MaxValue:    30.0,
			},
		},
		Handler:  HandleCrossfade,
		TextOnly: false,
		Usage:    cmd("crossfade").Usage,
		Example:  cmd("crossfade").Example,
	})
	registerCommandAliases("crossfade", cmd("crossfade"))

	RegisterCommand(&Command{
		Name:        "fadeonstop",
		Description: cmd("fadeonstop").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("fadeonstop").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleFadeOnStop,
		TextOnly: false,
		Usage:    cmd("fadeonstop").Usage,
		Example:  cmd("fadeonstop").Example,
	})
	registerCommandAliases("fadeonstop", cmd("fadeonstop"))

	RegisterCommand(&Command{
		Name:        "trimsilence",
		Description: cmd("trimsilence").Description,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "setting",
				Description: cmd("trimsilence").Options["setting"],
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "on", Value: "on"},
					{Name: "off", Value: "off"},
				},
			},
		},
		Handler:  HandleTrimSilence,
		TextOnly: false,
		Usage:    cmd("trimsilence").Usage,
		Example:  cmd("trimsilence").Example,
	})
	registerCommandAliases("trimsilence", cmd("trimsilence"))

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

	for _, langCmd := range []string{"setlanguage", "lang", "language"} {
		name := langCmd
		RegisterCommand(&Command{
			Name:        name,
			Description: cmd("setlanguage").Description,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "language",
					Description: cmd("setlanguage").Options["language"],
					Required:    false,
					Choices:     buildLanguageChoices(),
				},
			},
			Handler:   HandleSetLanguage,
			AdminOnly: true,
			TextOnly:  true,
			Usage:     cmd("setlanguage").Usage,
			Example:   cmd("setlanguage").Example,
		})
	}
	registerCommandAliases("setlanguage", cmd("setlanguage"))

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

	logger.Debug("[Commands] All commands registered")
}

func RegisterSlashCommands(session *discordgo.Session) error {
	logger.Debug("[Commands] Syncing slash commands with Discord...")

	appID := session.State.User.ID

	desired := make([]*discordgo.ApplicationCommand, 0, len(commands))
	for _, cmd := range commands {
		if cmd.TextOnly {
			logger.Debugf("[Commands] Skipping text-only command from slash registration: %s", cmd.Name)
			continue
		}
		desired = append(desired, &discordgo.ApplicationCommand{
			Name:        cmd.Name,
			Description: cmd.Description,
			Options:     cmd.Options,
		})
	}

	existing, err := session.ApplicationCommands(appID, "")
	if err != nil {
		return fmt.Errorf("failed to get existing commands: %w", err)
	}

	desiredJSON, err := canonicalCommandMap(desired)
	if err != nil {
		return fmt.Errorf("failed to canonicalize desired commands: %w", err)
	}
	existingJSON, err := canonicalCommandMap(existing)
	if err != nil {
		return fmt.Errorf("failed to canonicalize existing commands: %w", err)
	}

	added, updated, removed := diffCommandSets(desiredJSON, existingJSON)
	if len(added) == 0 && len(updated) == 0 && len(removed) == 0 {
		logger.Debug("[Commands] Slash commands already in sync, skipping registration")
		return nil
	}

	logger.Infof("[Commands] Slash command changes detected — added: %v, updated: %v, removed: %v", added, updated, removed)

	if _, err := session.ApplicationCommandBulkOverwrite(appID, "", desired); err != nil {
		return fmt.Errorf("failed to bulk overwrite slash commands: %w", err)
	}

	logger.Info("[Commands] Slash commands registered successfully")
	return nil
}

func canonicalCommandMap(cmds []*discordgo.ApplicationCommand) (map[string]string, error) {
	out := make(map[string]string, len(cmds))
	for _, cmd := range cmds {
		opts := cmd.Options
		if opts == nil {
			opts = []*discordgo.ApplicationCommandOption{}
		}
		shape := struct {
			Name        string                                `json:"name"`
			Description string                                `json:"description"`
			Options     []*discordgo.ApplicationCommandOption `json:"options"`
		}{cmd.Name, cmd.Description, opts}
		buf, err := json.Marshal(shape)
		if err != nil {
			return nil, fmt.Errorf("marshal command %q: %w", cmd.Name, err)
		}
		out[cmd.Name] = string(buf)
	}
	return out, nil
}

func diffCommandSets(desired, existing map[string]string) (added, updated, removed []string) {
	for name, want := range desired {
		got, ok := existing[name]
		if !ok {
			added = append(added, name)
		} else if got != want {
			updated = append(updated, name)
		}
	}
	for name := range existing {
		if _, ok := desired[name]; !ok {
			removed = append(removed, name)
		}
	}
	return added, updated, removed
}

func HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	cmdName := i.ApplicationCommandData().Name
	cmd, exists := commands[cmdName]
	if !exists {
		logger.Warnf("[Commands] Unknown command: %s", cmdName)
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.UnknownCommand))
		return
	}

	if cmd.AdminOnly {
		if !config.IsAdmin(i.Member.User.ID) && !isGuildAdmin(s, i.GuildID, i.Member) {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.NoPermission, messages.T(i.GuildID).Errors.AdminOnly))
			return
		}
	}

	logger.Debugf("[Commands] Executing command: %s (user: %s, guild: %s)",
		cmdName, i.Member.User.Username, i.GuildID)

	if err := cmd.Handler(s, i); err != nil {
		logger.Errorf("[Commands] Command %s failed: %v", cmdName, err)
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Errors.CommandExecutionError, err)))
	}
}

func HandleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {

	if m.Author.Bot {
		return
	}

	cfg := config.GetConfig()
	prefix := cfg.Prefix
	if m.GuildID != "" {
		if guildPrefix, err := queue.GetGuildPrefix(m.GuildID); err != nil {
			logger.Debugf("[HandleMessage] failed to get guild prefix for %s: %v", m.GuildID, err)
		} else if guildPrefix != "" {
			prefix = guildPrefix
		}
	}

	if !strings.HasPrefix(m.Content, prefix) {
		return
	}

	content := strings.TrimPrefix(m.Content, prefix)
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	_ = parts[1:]

	aliasTarget, ok := aliases[cmdName]
	if !ok {
		return
	}
	cmdName = aliasTarget

	cmd, exists := commands[cmdName]
	if !exists {
		return
	}

	if cmd.AdminOnly {

		member, err := s.State.Member(m.GuildID, m.Author.ID)
		if err != nil {

			member, err = s.GuildMember(m.GuildID, m.Author.ID)
		}

		isBotAdmin := config.IsAdmin(m.Author.ID)
		isServerAdmin := (err == nil) && isGuildAdmin(s, m.GuildID, member)

		if !isBotAdmin && !isServerAdmin {
			embed := messages.CreateErrorEmbed(messages.T(m.GuildID).Titles.NoPermission, messages.T(m.GuildID).Errors.AdminOnly)

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

	logger.Debugf("[Commands] Executing text command: %s (user: %s, guild: %s)",
		cmdName, m.Author.Username, m.GuildID)

	args := parts[1:]

	pseudoInteraction := CreatePseudoInteraction(s, m, cmd, args)

	messageResponder := &MessageResponse{
		Session:       s,
		ChannelID:     m.ChannelID,
		Message:       nil,
		OriginalMsgID: m.ID,
	}

	messageResponders.Store(pseudoInteraction.Token, messageResponder)
	defer messageResponders.Delete(pseudoInteraction.Token)

	if err := cmd.Handler(s, pseudoInteraction); err != nil {
		logger.Errorf("[Commands] Text command %s failed: %v", cmdName, err)

		if messageResponder.Message == nil {
			embed := messages.CreateErrorEmbed(messages.T(m.GuildID).Titles.Error, fmt.Sprintf(messages.T(m.GuildID).Errors.CommandExecutionError, err))

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

func isMessageCommand(i *discordgo.InteractionCreate) bool {
	return strings.HasPrefix(i.Token, "message_")
}

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

	return s.InteractionResponse(i.Interaction)
}

func DeferResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if isMessageCommand(i) {

		if mr, ok := messageResponders.Load(i.Token); ok {
			loadingEmbed := &discordgo.MessageEmbed{
				Color:       0xFFA500,
				Title:       messages.T(i.GuildID).Titles.Loading,
				Description: messages.T(i.GuildID).Descriptions.Loading,
			}
			mr.(*MessageResponse).SendEmbed(loadingEmbed)
		}
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
}

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

func UpdateResponseEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) error {
	if isMessageCommand(i) {

		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				_, err := s.ChannelMessageEditEmbed(responder.ChannelID, responder.Message.ID, embed)
				return err
			}
		}
		return fmt.Errorf("message responder or message not found")
	}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
	return err
}

func UpdateResponseEmbedWithComponents(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	if isMessageCommand(i) {

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

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
	return err
}

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

func checkUserInBotVoiceChannel(s *discordgo.Session, i *discordgo.InteractionCreate) (string, *discordgo.MessageEmbed) {

	voiceState, err := s.State.VoiceState(i.GuildID, i.Member.User.ID)
	if err != nil || voiceState.ChannelID == "" {
		return "", messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.NotInVoiceChannel)
	}

	q, err := queue.GetQueue(i.GuildID, false)
	if err != nil || q == nil || q.VoiceChannelID == "" {

		return voiceState.ChannelID, nil
	}

	if voiceState.ChannelID != q.VoiceChannelID {
		return "", messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Errors.MustBeInBotChannel)
	}

	return voiceState.ChannelID, nil
}

func buildLanguageChoices() []*discordgo.ApplicationCommandOptionChoice {
	codes := messages.AvailableLocales()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(codes))
	for _, code := range codes {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  code,
			Value: code,
		})
	}
	return choices
}
