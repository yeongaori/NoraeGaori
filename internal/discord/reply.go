package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func IsMessageCommand(i *discordgo.InteractionCreate) bool {
	return strings.HasPrefix(i.Token, "message_")
}

func RespondError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	if IsMessageCommand(i) {
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
	if IsMessageCommand(i) {
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
	if IsMessageCommand(i) {
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
	if IsMessageCommand(i) {
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
	if IsMessageCommand(i) {

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
	if IsMessageCommand(i) {
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
	if IsMessageCommand(i) {
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
	if IsMessageCommand(i) {

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
	if IsMessageCommand(i) {

		if mr, ok := messageResponders.Load(i.Token); ok {
			responder := mr.(*MessageResponse)
			if responder.Message != nil {
				logger.Debugf("Editing message %s in channel %s", responder.Message.ID, responder.ChannelID)
				_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:    responder.ChannelID,
					ID:         responder.Message.ID,
					Embeds:     &[]*discordgo.MessageEmbed{embed},
					Components: &components,
				})
				if err != nil {
					logger.Errorf("Failed to edit message: %v", err)
				}
				return err
			}
			logger.Errorf("Message is nil in responder")
			return fmt.Errorf("message is nil")
		}
		logger.Errorf("Message responder not found for token: %s", i.Token)
		return fmt.Errorf("message responder not found")
	}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
	return err
}

func GetResponseMessage(s *discordgo.Session, i *discordgo.InteractionCreate) (*discordgo.Message, error) {
	if IsMessageCommand(i) {
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
