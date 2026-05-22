package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/pkg/logger"
)

type MessageCommandAdapter struct {
	Message *discordgo.MessageCreate
	Session *discordgo.Session
	Command *Command
	Args    []string
}

func CreatePseudoInteraction(s *discordgo.Session, m *discordgo.MessageCreate, cmd *Command, args []string) *discordgo.InteractionCreate {
	
	member, err := s.GuildMember(m.GuildID, m.Author.ID)
	if err != nil {
		logger.Errorf("[MessageCommand] Failed to get member: %v", err)
		member = &discordgo.Member{
			User: m.Author,
		}
	}

	
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

func parseCommandOptions(cmd *Command, args []string) []*discordgo.ApplicationCommandInteractionDataOption {
	if len(cmd.Options) == 0 || len(args) == 0 {
		return nil
	}

	options := make([]*discordgo.ApplicationCommandInteractionDataOption, 0)
	argIndex := 0

	for _, opt := range cmd.Options {
		if argIndex >= len(args) {
			
			if opt.Required {
				
				break
			}
			continue
		}

		var value interface{}
		var consumed int

		switch opt.Type {
		case discordgo.ApplicationCommandOptionString:
			
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
				
				logger.Warnf("[MessageCommand] Invalid integer: %s", args[argIndex])
				argIndex++
				continue
			}
			
			value = float64(intVal)
			consumed = 1

		case discordgo.ApplicationCommandOptionBoolean:
			boolVal := args[argIndex] == "true" || args[argIndex] == "on" || args[argIndex] == "1"
			value = boolVal
			consumed = 1

		case discordgo.ApplicationCommandOptionChannel:
			
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

type MessageResponse struct {
	Session          *discordgo.Session
	ChannelID        string
	Message          *discordgo.Message
	OriginalMsgID    string 
}

func (mr *MessageResponse) SendEmbed(embed *discordgo.MessageEmbed) {
	
	msg, err := mr.Session.ChannelMessageSendComplex(mr.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Reference: &discordgo.MessageReference{
			MessageID: mr.OriginalMsgID,
			ChannelID: mr.ChannelID,
		},
	})
	if err == nil {
		mr.Message = msg 
		logger.Debugf("[MessageResponse] Sent reply and stored message ID: %s", msg.ID)
	} else {
		logger.Errorf("[MessageResponse] Failed to send embed reply: %v", err)
	}
}

func (mr *MessageResponse) SendMessage(content string) {
	
	msg, err := mr.Session.ChannelMessageSendComplex(mr.ChannelID, &discordgo.MessageSend{
		Content: content,
		Reference: &discordgo.MessageReference{
			MessageID: mr.OriginalMsgID,
			ChannelID: mr.ChannelID,
		},
	})
	if err == nil {
		mr.Message = msg 
	}
}

func (mr *MessageResponse) SendFollowUp(content string) {
	mr.Session.ChannelMessageSend(mr.ChannelID, content)
}

func (mr *MessageResponse) SendFollowUpEmbed(embed *discordgo.MessageEmbed) {
	mr.Session.ChannelMessageSendEmbed(mr.ChannelID, embed)
}

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
		mr.Message = msg 
	}
	return msg, err
}
