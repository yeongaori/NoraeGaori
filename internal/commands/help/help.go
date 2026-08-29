package help

import (
	"fmt"
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

const (
	helpPanelExpiry = 5 * time.Minute

	helpPrevPrefix = "help_prev_"
	helpNextPrefix = "help_next_"
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
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		page = int(options[0].IntValue())
	}

	prefix := config.GetConfig().Prefix
	if i.GuildID != "" {
		if guildPrefix, err := guild.GetPrefix(i.GuildID); err != nil {
			logger.Debugf("failed to get guild prefix for %s: %v", i.GuildID, err)
		} else if guildPrefix != "" {
			prefix = guildPrefix
		}
	}

	commandList := getAllCommands(i.GuildID)

	isAdmin := config.IsAdmin(i.Member.User.ID)
	filteredCommands := make([]CommandInfo, 0, len(commandList))
	for _, cmd := range commandList {
		if !cmd.AdminOnly || isAdmin {
			filteredCommands = append(filteredCommands, cmd)
		}
	}

	if len(filteredCommands) == 0 {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Help.NoCommandsTitle, messages.T(i.GuildID).Help.NoCommandsDesc))
		return nil
	}

	const commandsPerPage = 5
	totalPages := (len(filteredCommands) + commandsPerPage - 1) / commandsPerPage

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * commandsPerPage
	end := start + commandsPerPage
	if end > len(filteredCommands) {
		end = len(filteredCommands)
	}
	pageCommands := filteredCommands[start:end]

	embed := buildHelpEmbed(i.GuildID, pageCommands, page, totalPages, start, len(filteredCommands), prefix)

	if totalPages == 1 {
		discord.RespondEmbed(s, i, embed)
		return nil
	}

	panel := &helpPanel{
		guildID:    i.GuildID,
		token:      discord.NewComponentToken(),
		totalPages: totalPages,
		perPage:    commandsPerPage,
		prefix:     prefix,
		commands:   filteredCommands,
		page:       page,
	}

	components := createHelpButtons(i.GuildID, page, totalPages, panel.token)

	removeHandler := s.AddHandler(panel.handleInteraction)

	panelMsg, err := discord.RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		removeHandler()
		logger.Errorf("Failed to send response: %v", err)
		return err
	}

	go expireHelpPanel(s, i, panelMsg, panel, removeHandler)

	return nil
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

		aliasesStr := strings.Join(cmd.Aliases, ", ")

		description.WriteString(fmt.Sprintf("**%d. %s%s**\n", position, adminBadge, cmd.Name))
		description.WriteString(fmt.Sprintf("%s\n", cmd.Description))
		description.WriteString(fmt.Sprintf(t.Help.MessageLabel+"\n", prefix, cmd.Usage))
		description.WriteString(fmt.Sprintf(t.Help.AliasLabel+"\n", aliasesStr))
		description.WriteString(fmt.Sprintf(t.Help.SlashLabel+"\n", cmd.Name))
		if cmd.Example != "" {
			description.WriteString(fmt.Sprintf(t.Help.ExampleLabel+"\n", prefix, cmd.Example))
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

func createHelpButtons(guildID string, page, totalPages int, token string) []discordgo.MessageComponent {
	t := messages.T(guildID)
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    t.Buttons.Previous,
					Style:    discordgo.PrimaryButton,
					CustomID: helpPrevPrefix + token,
					Disabled: page == 1,
				},
				discordgo.Button{
					Label:    t.Buttons.Next,
					Style:    discordgo.PrimaryButton,
					CustomID: helpNextPrefix + token,
					Disabled: page == totalPages,
				},
			},
		},
	}
}

type helpPanel struct {
	guildID    string
	token      string
	totalPages int
	perPage    int
	prefix     string
	commands   []CommandInfo

	pageMu sync.Mutex
	page   int
}

func (panel *helpPanel) turnPage(customID string) (int, bool) {
	panel.pageMu.Lock()
	defer panel.pageMu.Unlock()

	switch customID {
	case helpPrevPrefix + panel.token:
		if panel.page > 1 {
			panel.page--
		}
	case helpNextPrefix + panel.token:
		if panel.page < panel.totalPages {
			panel.page++
		}
	default:
		return 0, false
	}
	return panel.page, true
}

func (panel *helpPanel) render(page int) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	start := (page - 1) * panel.perPage
	end := start + panel.perPage
	if end > len(panel.commands) {
		end = len(panel.commands)
	}

	embed := buildHelpEmbed(panel.guildID, panel.commands[start:end], page, panel.totalPages, start, len(panel.commands), panel.prefix)
	return embed, createHelpButtons(panel.guildID, page, panel.totalPages, panel.token)
}

func (panel *helpPanel) handleInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if ic.Type != discordgo.InteractionMessageComponent || ic.GuildID != panel.guildID {
		return
	}

	page, turned := panel.turnPage(ic.MessageComponentData().CustomID)
	if !turned {
		return
	}

	embed, components := panel.render(page)
	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	}); err != nil {
		logger.Errorf("Failed to turn the help page: %v", err)
	}
}

func expireHelpPanel(s *discordgo.Session, i *discordgo.InteractionCreate, panelMsg *discordgo.Message, panel *helpPanel, removeHandler func()) {
	defer removeHandler()

	<-time.After(helpPanelExpiry)

	panelMsg = discord.ResolvePanelMessage(s, i, panelMsg)
	if panelMsg == nil {
		return
	}

	panel.pageMu.Lock()
	page := panel.page
	panel.pageMu.Unlock()

	embed, _ := panel.render(page)
	if _, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         panelMsg.ID,
		Channel:    panelMsg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	}); err != nil {
		logger.Errorf("Failed to close the help panel: %v", err)
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

		cmdAliases := []string{name}
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
