package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
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
		if guildPrefix, err := queue.GetGuildPrefix(i.GuildID); err != nil {
			logger.Debugf("[Help] failed to get guild prefix for %s: %v", i.GuildID, err)
		} else if guildPrefix != "" {
			prefix = guildPrefix
		}
	}

	
	commandList := getAllCommands(i.GuildID)

	
	isAdmin := config.IsAdmin(i.Member.User.ID)
	filteredCommands := make([]CommandInfo, 0)
	for _, cmd := range commandList {
		if !cmd.AdminOnly || isAdmin {
			filteredCommands = append(filteredCommands, cmd)
		}
	}

	if len(filteredCommands) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Help.NoCommandsTitle, messages.T(i.GuildID).Help.NoCommandsDesc))
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
		RespondEmbed(s, i, embed)
		return nil
	}

	components := createHelpButtons(i.GuildID, page, totalPages)

	
	msg, err := RespondEmbedWithComponents(s, i, embed, components)
	if err != nil {
		logger.Errorf("[Help] Failed to send response: %v", err)
		return err
	}

	
	go handleHelpButtons(s, i, msg, i.GuildID, totalPages, commandsPerPage, filteredCommands, prefix)

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

func createHelpButtons(guildID string, page, totalPages int) []discordgo.MessageComponent {
	t := messages.T(guildID)
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    t.Buttons.Previous,
					Style:    discordgo.PrimaryButton,
					CustomID: "help_prev",
					Disabled: page == 1,
				},
				discordgo.Button{
					Label:    t.Buttons.Next,
					Style:    discordgo.PrimaryButton,
					CustomID: "help_next",
					Disabled: page == totalPages,
				},
			},
		},
	}
}

func handleHelpButtons(s *discordgo.Session, i *discordgo.InteractionCreate, originalMsg *discordgo.Message, guildID string, totalPages, perPage int, allCommands []CommandInfo, prefix string) {
	timeout := time.After(5 * time.Minute)
	currentPage := 1

	
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		currentPage = int(options[0].IntValue())
	}

	
	originalMsgID := ""
	if originalMsg != nil {
		originalMsgID = originalMsg.ID
	}

	
	buttonHandler := func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic.Type != discordgo.InteractionMessageComponent {
			return
		}

		
		if originalMsgID != "" && (ic.Message == nil || ic.Message.ID != originalMsgID) {
			return
		}

		
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

		
		start := (currentPage - 1) * perPage
		end := start + perPage
		if end > len(allCommands) {
			end = len(allCommands)
		}
		pageCommands := allCommands[start:end]

		embed := buildHelpEmbed(guildID, pageCommands, currentPage, totalPages, start, len(allCommands), prefix)

		components := createHelpButtons(guildID, currentPage, totalPages)

		
		s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	}

	
	removeHandler := s.AddHandler(buttonHandler)
	defer removeHandler()

	
	<-timeout

	
	start := (currentPage - 1) * perPage
	end := start + perPage
	if end > len(allCommands) {
		end = len(allCommands)
	}
	pageCommands := allCommands[start:end]

	embed := buildHelpEmbed(guildID, pageCommands, currentPage, totalPages, start, len(allCommands), prefix)

	
	var msg *discordgo.Message
	if i.Interaction.Message != nil {
		msg = i.Interaction.Message
	} else {
		
		msg, err := GetResponseMessage(s, i)
		if err != nil {
			return
		}
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         msg.ID,
			Channel:    msg.ChannelID,
			Embeds:     &[]*discordgo.MessageEmbed{embed},
			Components: &[]discordgo.MessageComponent{}, 
		})
		return
	}

	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         msg.ID,
		Channel:    msg.ChannelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{}, 
	})
}

func getAllCommands(guildID string) []CommandInfo {
	commandList := make([]CommandInfo, 0)

	t := messages.T(guildID)

	for name, cmd := range commands {
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
