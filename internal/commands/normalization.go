package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func HandleNormalization(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var enabled bool
	if len(options) > 0 {
		mode := options[0].StringValue()
		enabled = mode == "on"
	} else {
		
		currentNormalization, err := queue.GetNormalization(i.GuildID)
		if err != nil {
			logger.Errorf("[Normalization] Failed to get current state: %v", err)
			currentNormalization = false
		}
		enabled = !currentNormalization
	}

	if err := queue.SetNormalization(i.GuildID, enabled); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Settings.NormalizationError, err)))
		return err
	}

	
	player.RestartForNormalization(i.GuildID)

	statusText := messages.T(i.GuildID).Settings.StatusOff
	statusEmoji := ""
	color := messages.ColorError
	if enabled {
		statusText = messages.T(i.GuildID).Settings.StatusOn
		statusEmoji = ""
		color = messages.ColorSuccess
	}

	embed := &discordgo.MessageEmbed{
		Color:       color,
		Title:       fmt.Sprintf(messages.T(i.GuildID).Settings.NormalizationTitle, statusEmoji, statusText),
		Description: fmt.Sprintf(messages.T(i.GuildID).Settings.NormalizationDesc, statusText),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T(i.GuildID).Settings.NormalizationWhatTitle,
				Value:  messages.T(i.GuildID).Settings.NormalizationWhatDesc,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}
