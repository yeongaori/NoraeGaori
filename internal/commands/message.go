package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/pkg/logger"
)

// MessageCommandAdapter converts a text message to a pseudo-interaction
type MessageCommandAdapter struct {
	Message *discordgo.MessageCreate
	Session *discordgo.Session
	Command *Command
	Args    []string
}

// CreatePseudoInteraction creates a pseudo-interaction from a message
func CreatePseudoInteraction(s *discordgo.Session, m *discordgo.MessageCreate, cmd *Command, args []string) *discordgo.InteractionCreate {
	// Get member info
	member, err := s.GuildMember(m.GuildID, m.Author.ID)
	if err != nil {
		logger.Errorf("[MessageCommand] Failed to get member: %v", err)
		member = &discordgo.Member{
			User: m.Author,
		}
	}

	// Parse command options based on args
	options := parseCommandOptions(cmd, args)

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: m.GuildID,
			Member:  member,
			ChannelID: m.ChannelID,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    cmd.Name,
				Options: options,
			},
			Token: fmt.Sprintf("message_%s_%s", m.ID, m.ChannelID),
		},
	}

	return interaction
}

// parseCommandOptions converts string args to ApplicationCommandInteractionDataOption
func parseCommandOptions(cmd *Command, args []string) []*discordgo.ApplicationCommandInteractionDataOption {
	if len(cmd.Options) == 0 || len(args) == 0 {
		return nil
	}

	options := make([]*discordgo.ApplicationCommandInteractionDataOption, 0)
	argIndex := 0

	for _, opt := range cmd.Options {
		if argIndex >= len(args) {
			// No more args, check if option is required
			if opt.Required {
				// Cannot continue without required option
				break
			}
			continue
		}

		var value interface{}
		var consumed int

		switch opt.Type {
		case discordgo.ApplicationCommandOptionString:
			// For string options, if it's the last option or rest of command, take all remaining args
			if argIndex == len(cmd.Options)-1 || opt.Name == "query" || opt.Name == "position" || opt.Name == "target" {
				value = strings.Join(args[argIndex:], " ")
				consumed = len(args) - argIndex
			} else {
				value = args[argIndex]
				consumed = 1
			}

		case discordgo.ApplicationCommandOptionInteger:
			intVal, err := strconv.ParseInt(args[argIndex], 10, 64)
			if err != nil {
				// Invalid integer, skip this option
				logger.Warnf("[MessageCommand] Invalid integer: %s", args[argIndex])
				argIndex++
				continue
			}
			// Discord API represents integers as float64, so we need to convert
			value = float64(intVal)
			consumed = 1

		case discordgo.ApplicationCommandOptionBoolean:
			boolVal := args[argIndex] == "true" || args[argIndex] == "on" || args[argIndex] == "1"
			value = boolVal
			consumed = 1

		case discordgo.ApplicationCommandOptionChannel:
			// Parse channel mention <#channelID>
			channelID := args[argIndex]
			if strings.HasPrefix(channelID, "<#") && strings.HasSuffix(channelID, ">") {
				channelID = strings.TrimPrefix(channelID, "<#")
				channelID = strings.TrimSuffix(channelID, ">")
			}
			value = channelID
			consumed = 1

		default:
			value = args[argIndex]
			consumed = 1
		}

		options = append(options, &discordgo.ApplicationCommandInteractionDataOption{
			Name:  opt.Name,
			Type:  opt.Type,
			Value: value,
		})

		argIndex += consumed
	}

	return options
}

// MessageResponse wraps response functions for message-based commands
type MessageResponse struct {
	Session          *discordgo.Session
	ChannelID        string
	Message          *discordgo.Message
	OriginalMsgID    string // Original message ID to reply to
}

// SendEmbed sends an embed as a response (reply) to a message command
func (mr *MessageResponse) SendEmbed(embed *discordgo.MessageEmbed) {
	// Use MessageSendComplex with Reference to reply to the original message
	msg, err := mr.Session.ChannelMessageSendComplex(mr.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Reference: &discordgo.MessageReference{
			MessageID: mr.OriginalMsgID,
			ChannelID: mr.ChannelID,
		},
	})
	if err == nil {
		mr.Message = msg // Store the sent message for later editing
		logger.Debugf("[MessageResponse] Sent reply and stored message ID: %s", msg.ID)
	} else {
		logger.Errorf("[MessageResponse] Failed to send embed reply: %v", err)
	}
}

// SendMessage sends a text message as a response (reply)
func (mr *MessageResponse) SendMessage(content string) {
	// Use MessageSendComplex with Reference to reply to the original message
	msg, err := mr.Session.ChannelMessageSendComplex(mr.ChannelID, &discordgo.MessageSend{
		Content: content,
		Reference: &discordgo.MessageReference{
			MessageID: mr.OriginalMsgID,
			ChannelID: mr.ChannelID,
		},
	})
	if err == nil {
		mr.Message = msg // Store the sent message for later editing
	}
}

// SendFollowUp sends a follow-up message
func (mr *MessageResponse) SendFollowUp(content string) {
	mr.Session.ChannelMessageSend(mr.ChannelID, content)
}

// SendFollowUpEmbed sends a follow-up embed
func (mr *MessageResponse) SendFollowUpEmbed(embed *discordgo.MessageEmbed) {
	mr.Session.ChannelMessageSendEmbed(mr.ChannelID, embed)
}

// SendEmbedWithComponents sends an embed with message components (buttons, select menus, etc.) as a reply
func (mr *MessageResponse) SendEmbedWithComponents(embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	msg, err := mr.Session.ChannelMessageSendComplex(mr.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
		Reference: &discordgo.MessageReference{
			MessageID: mr.OriginalMsgID,
			ChannelID: mr.ChannelID,
		},
	})
	if err == nil {
		mr.Message = msg // Store the sent message for later editing
	}
	return msg, err
}
