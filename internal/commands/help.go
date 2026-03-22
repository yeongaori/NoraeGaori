package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/pkg/logger"
)

// CommandInfo represents information about a command for help display
type CommandInfo struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Example     string
	AdminOnly   bool
}

// HandleHelp handles the help command with pagination
func HandleHelp(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	// Get page from options if provided
	page := 1
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		page = int(options[0].IntValue())
	}

	// Get current prefix
	cfg := config.GetConfig()
	prefix := cfg.Prefix

	// Get all commands
	commandList := getAllCommands()

	// Filter admin commands for non-admins
	isAdmin := config.IsAdmin(i.Member.User.ID)
	filteredCommands := make([]CommandInfo, 0)
	for _, cmd := range commandList {
		if !cmd.AdminOnly || isAdmin {
			filteredCommands = append(filteredCommands, cmd)
		}
	}

	if len(filteredCommands) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T().Help.NoCommandsTitle, messages.T().Help.NoCommandsDesc))
		return nil
	}

	// Pagination settings
	const commandsPerPage = 5
	totalPages := (len(filteredCommands) + commandsPerPage - 1) / commandsPerPage

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// Get commands for current page
	start := (page - 1) * commandsPerPage
	end := start + commandsPerPage
	if end > len(filteredCommands) {
		end = len(filteredCommands)
	}
	pageCommands := filteredCommands[start:end]

	embed := buildHelpEmbed(pageCommands, page, totalPages, start, len(filteredCommands), prefix)

	// Create navigation buttons if there are multiple pages
	if totalPages == 1 {
		RespondEmbed(s, i, embed)
		return nil
	}

	components := createHelpButtons(page, totalPages)

	// Send response with buttons using the new helper
	msg, err := RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		logger.Errorf("[Help] Failed to send response: %v", err)
		return err
	}

	// Start button collector (5 minute timeout)
	go handleHelpButtons(s, i, msg, i.GuildID, totalPages, commandsPerPage, filteredCommands, prefix)

	return nil
}

// buildHelpEmbed constructs a help embed for the given page of commands.
func buildHelpEmbed(commands []CommandInfo, page, totalPages, startIndex, totalCommands int, prefix string) *discordgo.MessageEmbed {
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
		description.WriteString(fmt.Sprintf(messages.T().Help.MessageLabel+"\n", prefix, cmd.Usage))
		description.WriteString(fmt.Sprintf(messages.T().Help.AliasLabel+"\n", aliasesStr))
		description.WriteString(fmt.Sprintf(messages.T().Help.SlashLabel+"\n", cmd.Name))
		if cmd.Example != "" {
			description.WriteString(fmt.Sprintf(messages.T().Help.ExampleLabel+"\n", prefix, cmd.Example))
		}
		description.WriteString("\n")
	}

	return &discordgo.MessageEmbed{
		Color:       messages.ColorInfo,
		Title:       messages.TitleHelp,
		Description: description.String(),
		Fields: []*discordgo.MessageEmbedField{
			{Name: messages.FieldCurrentPrefix, Value: fmt.Sprintf("`%s`", prefix), Inline: true},
			{Name: messages.FieldTotalCommands, Value: fmt.Sprintf(messages.T().Help.TotalCommandsValue, totalCommands), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(messages.FooterHelpPagination, page, totalPages),
		},
	}
}

// createHelpButtons creates Previous/Next buttons for help pagination
func createHelpButtons(page, totalPages int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    messages.ButtonPrevious,
					Style:    discordgo.PrimaryButton,
					CustomID: "help_prev",
					Disabled: page == 1,
				},
				discordgo.Button{
					Label:    messages.ButtonNext,
					Style:    discordgo.PrimaryButton,
					CustomID: "help_next",
					Disabled: page == totalPages,
				},
			},
		},
	}
}

// handleHelpButtons handles button interactions for help pagination
func handleHelpButtons(s *discordgo.Session, i *discordgo.InteractionCreate, originalMsg *discordgo.Message, guildID string, totalPages, perPage int, allCommands []CommandInfo, prefix string) {
	timeout := time.After(5 * time.Minute)
	currentPage := 1

	// Get initial page from options
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		currentPage = int(options[0].IntValue())
	}

	// Get message ID for comparison
	originalMsgID := ""
	if originalMsg != nil {
		originalMsgID = originalMsg.ID
	}

	// Create a channel for button events
	buttonHandler := func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic.Type != discordgo.InteractionMessageComponent {
			return
		}

		// Check if this is our message (if we have a message ID to check against)
		if originalMsgID != "" && (ic.Message == nil || ic.Message.ID != originalMsgID) {
			return
		}

		// Check if it's a help button by CustomID
		data := ic.MessageComponentData()
		if data.CustomID != "help_prev" && data.CustomID != "help_next" {
			return
		}

		switch data.CustomID {
		case "help_prev":
			if currentPage > 1 {
				currentPage--
			}
		case "help_next":
			if currentPage < totalPages {
				currentPage++
			}
		default:
			return
		}

		// Build updated embed
		start := (currentPage - 1) * perPage
		end := start + perPage
		if end > len(allCommands) {
			end = len(allCommands)
		}
		pageCommands := allCommands[start:end]

		embed := buildHelpEmbed(pageCommands, currentPage, totalPages, start, len(allCommands), prefix)

		components := createHelpButtons(currentPage, totalPages)

		// Respond to button interaction
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	}

	// Register handler
	removeHandler := s.AddHandler(buttonHandler)
	defer removeHandler()

	// Wait for timeout
	<-timeout

	// Remove buttons after timeout
	start := (currentPage - 1) * perPage
	end := start + perPage
	if end > len(allCommands) {
		end = len(allCommands)
	}
	pageCommands := allCommands[start:end]

	embed := buildHelpEmbed(pageCommands, currentPage, totalPages, start, len(allCommands), prefix)

	// Get the message to edit
	var msg *discordgo.Message
	if i.Interaction.Message != nil {
		msg = i.Interaction.Message
	} else {
		// For deferred responses, get the original response
		msg, err := GetResponseMessage(s, i)
		if err != nil {
			return
		}
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         msg.ID,
			Channel:    msg.ChannelID,
			Embeds:     &[]*discordgo.MessageEmbed{embed},
			Components: &[]discordgo.MessageComponent{}, // Remove buttons
		})
		return
	}

	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         msg.ID,
		Channel:    msg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{}, // Remove buttons
	})
}

// getAllCommands returns all registered commands with their information
func getAllCommands() []CommandInfo {
	commandList := make([]CommandInfo, 0)

	// Build alias map (reverse lookup)
	aliasMap := make(map[string][]string) // command name -> aliases
	for alias, cmdName := range aliases {
		aliasMap[cmdName] = append(aliasMap[cmdName], alias)
	}

	for name, cmd := range commands {
		// Get all aliases for this command (include command name)
		cmdAliases := []string{name}
		cmdAliases = append(cmdAliases, aliasMap[name]...)

		// Use predefined usage and example if available, otherwise fall back to defaults
		usage := cmd.Usage
		if usage == "" {
			usage = name
		}

		example := cmd.Example
		if example == "" {
			example = name
		}

		commandList = append(commandList, CommandInfo{
			Name:        name,
			Aliases:     cmdAliases,
			Description: cmd.Description,
			Usage:       usage,
			Example:     example,
			AdminOnly:   cmd.AdminOnly,
		})
	}

	// Sort commands alphabetically
	sort.Slice(commandList, func(i, j int) bool {
		return commandList[i].Name < commandList[j].Name
	})

	return commandList
}
