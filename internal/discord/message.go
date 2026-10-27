package discord

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
)

func MessageMember(s *discordgo.Session, m *discordgo.MessageCreate) *discordgo.Member {
	if m.Member != nil {
		member := *m.Member
		member.User = m.Author
		member.GuildID = m.GuildID
		return &member
	}

	member, err := s.GuildMember(m.GuildID, m.Author.ID)
	if err != nil {
		logger.Errorf("Failed to get member: %v", err)
		return &discordgo.Member{User: m.Author}
	}
	return member
}

func CreatePseudoInteraction(m *discordgo.MessageCreate, member *discordgo.Member, name string, opts []*discordgo.ApplicationCommandOption, args []string) *discordgo.InteractionCreate {
	options := parseCommandOptions(opts, args)

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:      discordgo.InteractionApplicationCommand,
			GuildID:   m.GuildID,
			Member:    member,
			ChannelID: m.ChannelID,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    name,
				Options: options,
			},
			Token: fmt.Sprintf("message_%s_%s", m.ID, m.ChannelID),
		},
	}

	return interaction
}

func parseOptionValue(opt *discordgo.ApplicationCommandOption, args []string, argIndex, optionCount int) (interface{}, int, bool) {
	switch opt.Type {
	case discordgo.ApplicationCommandOptionString:

		if argIndex == optionCount-1 || opt.Name == "query" || opt.Name == "position" || opt.Name == "target" {
			return strings.Join(args[argIndex:], " "), len(args) - argIndex, true
		}
		return args[argIndex], 1, true

	case discordgo.ApplicationCommandOptionInteger:
		intVal, err := strconv.ParseInt(args[argIndex], 10, 64)
		if err != nil {

			logger.Warnf("Invalid integer: %s", args[argIndex])
			return nil, 0, false
		}

		return float64(intVal), 1, true

	case discordgo.ApplicationCommandOptionBoolean:
		boolVal := args[argIndex] == "true" || args[argIndex] == "on" || args[argIndex] == "1"
		return boolVal, 1, true

	case discordgo.ApplicationCommandOptionChannel:
		return trimChannelMention(args[argIndex]), 1, true

	default:
		return args[argIndex], 1, true
	}
}

func trimChannelMention(channelID string) string {
	if strings.HasPrefix(channelID, "<#") && strings.HasSuffix(channelID, ">") {
		channelID = strings.TrimPrefix(channelID, "<#")
		channelID = strings.TrimSuffix(channelID, ">")
	}
	return channelID
}

func parseCommandOptions(opts []*discordgo.ApplicationCommandOption, args []string) []*discordgo.ApplicationCommandInteractionDataOption {
	if len(opts) == 0 || len(args) == 0 {
		return nil
	}

	options := make([]*discordgo.ApplicationCommandInteractionDataOption, 0)
	argIndex := 0

	for _, opt := range opts {
		if argIndex >= len(args) {

			if opt.Required {

				break
			}
			continue
		}

		value, consumed, ok := parseOptionValue(opt, args, argIndex, len(opts))
		if !ok {
			argIndex++
			continue
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

var messageResponders sync.Map

func RegisterResponder(token string, response *MessageResponse) func() {
	messageResponders.Store(token, response)
	return func() { messageResponders.Delete(token) }
}

type MessageResponse struct {
	Session       *discordgo.Session
	ChannelID     string
	Message       *discordgo.Message
	OriginalMsgID string
}

func SendReply(s *discordgo.Session, channelID, messageID string, send *discordgo.MessageSend) (*discordgo.Message, error) {
	send.Reference = &discordgo.MessageReference{
		MessageID: messageID,
		ChannelID: channelID,
	}
	return s.ChannelMessageSendComplex(channelID, send)
}

func (mr *MessageResponse) SendEmbed(embed *discordgo.MessageEmbed) {

	msg, err := SendReply(mr.Session, mr.ChannelID, mr.OriginalMsgID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	if err == nil {
		mr.Message = msg
		logger.Debugf("Sent reply and stored message ID: %s", msg.ID)
	} else {
		logger.Errorf("Failed to send embed reply: %v", err)
	}
}

func (mr *MessageResponse) resolveReply() *discordgo.Message {
	if mr.Message != nil {
		return mr.Message
	}
	if mr.OriginalMsgID == "" || mr.Session.State == nil || mr.Session.State.User == nil {
		return nil
	}

	recent, err := mr.Session.ChannelMessages(mr.ChannelID, 10, "", mr.OriginalMsgID, "")
	if err != nil {
		logger.Debugf("Failed to look up the reply to message %s: %v", mr.OriginalMsgID, err)
		return nil
	}

	var newestID uint64
	for _, msg := range recent {
		if !mr.isOwnReply(msg) {
			continue
		}
		id, err := strconv.ParseUint(msg.ID, 10, 64)
		if err != nil || id <= newestID {
			continue
		}
		newestID = id
		mr.Message = msg
	}
	return mr.Message
}

func (mr *MessageResponse) isOwnReply(msg *discordgo.Message) bool {
	return msg.Author != nil && msg.Author.ID == mr.Session.State.User.ID &&
		msg.MessageReference != nil && msg.MessageReference.MessageID == mr.OriginalMsgID
}

func (mr *MessageResponse) SendFollowUp(content string) {
	_, _ = mr.Session.ChannelMessageSend(mr.ChannelID, content)
}

func (mr *MessageResponse) SendFollowUpEmbed(embed *discordgo.MessageEmbed) {
	_, _ = mr.Session.ChannelMessageSendEmbed(mr.ChannelID, embed)
}

func (mr *MessageResponse) SendPanel(embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	msg, err := SendReply(mr.Session, mr.ChannelID, mr.OriginalMsgID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err == nil {
		mr.Message = msg
	}
	return msg, err
}
