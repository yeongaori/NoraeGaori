package help

import (
	"fmt"
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"slices"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

const (
	helpPageRoute   = "help_page"
	commandsPerPage = 5
)

type CommandInfo struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Example     string
	AdminOnly   bool
}

func HandleHelp(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	page := 1
	if options := i.ApplicationCommandData().Options; len(options) > 0 {
		page = int(options[0].IntValue())
	}

	embed, components, hasCommands := buildHelpPage(i.GuildID, config.IsAdmin(i.Member.User.ID), page)
	switch {
	case !hasCommands:
		t := messages.T(i.GuildID)
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Help.NoCommandsTitle, t.Help.NoCommandsDesc))
	case components == nil:
		discord.RespondEmbed(s, i, embed)
	default:
		if _, err := discord.SendEmbedWithComponents(s, i, embed, components); err != nil {
			logger.Errorf("Failed to send response: %v", err)
			return err
		}
	}
	return nil
}

func turnHelpPage(s *discordgo.Session, ic *discordgo.InteractionCreate, arguments []string) {
	page, hasPage := discord.PageArgument(ic, arguments, 2)
	if !hasPage {
		return
	}
	isAdmin, isValidView := discord.ParseViewArgument(arguments[0])
	if !isValidView {
		return
	}

	embed, components, hasCommands := buildHelpPage(ic.GuildID, isAdmin, page)
	if !hasCommands {
		return
	}
	if err := discord.UpdateComponentMessage(s, ic, embed, components); err != nil {
		logger.Errorf("Failed to turn the help page: %v", err)
	}
}

func buildHelpPage(guildID string, isAdmin bool, page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent, bool) {
	commands := visibleCommands(guildID, isAdmin)
	if len(commands) == 0 {
		return nil, nil, false
	}

	totalPages := discord.PageCount(len(commands), commandsPerPage)
	page = discord.ClampPage(page, totalPages)
	start, end := discord.PageBounds(page, commandsPerPage, len(commands))

	embed := buildHelpEmbed(guildID, commands[start:end], page, totalPages, start, len(commands), guildPrefix(guildID))
	if totalPages == 1 {
		return embed, nil, true
	}

	t := messages.T(guildID)
	row := discord.PageButtonRow(helpPageRoute, page, totalPages, t.Buttons.Previous, t.Buttons.Next, []string{discord.ViewArgument(isAdmin)})
	return embed, []discordgo.MessageComponent{row}, true
}

func visibleCommands(guildID string, isAdmin bool) []CommandInfo {
	commands := getAllCommands(guildID)
	if isAdmin {
		return commands
	}
	return slices.DeleteFunc(commands, func(cmd CommandInfo) bool { return cmd.AdminOnly })
}

func guildPrefix(guildID string) string {
	prefix := config.GetConfig().Prefix
	if guildID == "" {
		return prefix
	}

	stored, err := guild.GetPrefix(guildID)
	if err != nil {
		logger.Debugf("failed to get guild prefix for %s: %v", guildID, err)
		return prefix
	}
	if stored != "" {
		return stored
	}
	return prefix
}

func buildHelpEmbed(guildID string, commands []CommandInfo, page, totalPages, startIndex, totalCommands int, prefix string) *discordgo.MessageEmbed {
	t := messages.T(guildID)
	var description strings.Builder
	for idx, cmd := range commands {
		position := startIndex + idx + 1

		adminBadge := ""
		if cmd.AdminOnly {
			adminBadge = "🔴 "
		}

		fmt.Fprintf(&description, "**%d. %s%s**\n", position, adminBadge, cmd.Name)
		fmt.Fprintf(&description, "%s\n", cmd.Description)
		fmt.Fprintf(&description, t.Help.MessageLabel+"\n", prefix, cmd.Usage)
		fmt.Fprintf(&description, t.Help.AliasLabel+"\n", strings.Join(cmd.Aliases, ", "))
		fmt.Fprintf(&description, t.Help.SlashLabel+"\n", cmd.Name)
		if cmd.Example != "" {
			fmt.Fprintf(&description, t.Help.ExampleLabel+"\n", prefix, cmd.Example)
		}
		description.WriteString("\n")
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       t.Titles.Help,
		Description: description.String(),
		Fields: []*discordgo.MessageEmbedField{
			{Name: t.Fields.CurrentPrefix, Value: fmt.Sprintf("`%s`", prefix), Inline: true},
			{Name: t.Fields.TotalCommands, Value: fmt.Sprintf(t.Help.TotalCommandsValue, totalCommands), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(t.Footers.HelpPagination, page, totalPages),
		},
	}
}

func getAllCommands(guildID string) []CommandInfo {
	snapshot := command.Snapshot()
	commandList := make([]CommandInfo, 0, len(snapshot))

	t := messages.T(guildID)

	for name, cmd := range snapshot {
		var cs messages.CommandStrings
		if t != nil {
			cs = t.Commands[name]
		}

		description := cs.Description
		if description == "" {
			description = cmd.Description
		}

		usage := cs.Usage
		if usage == "" {
			usage = cmd.Usage
		}
		if usage == "" {
			usage = name
		}

		example := cs.Example
		if example == "" {
			example = cmd.Example
		}
		if example == "" {
			example = name
		}

		cmdAliases := make([]string, 0, len(cs.Aliases)+1)
		cmdAliases = append(cmdAliases, name)
		cmdAliases = append(cmdAliases, cs.Aliases...)

		commandList = append(commandList, CommandInfo{
			Name:        name,
			Aliases:     cmdAliases,
			Description: description,
			Usage:       usage,
			Example:     example,
			AdminOnly:   cmd.AdminOnly,
		})
	}

	sort.Slice(commandList, func(i, j int) bool {
		return commandList[i].Name < commandList[j].Name
	})

	return commandList
}
