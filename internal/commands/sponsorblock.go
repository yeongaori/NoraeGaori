package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func HandleSponsorBlock(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var enabled bool
	if len(options) > 0 {
		mode := options[0].StringValue()
		enabled = mode == "on"
	} else {
		// No arg → toggle the current value.
		currentSponsorBlock, err := queue.GetSponsorBlock(i.GuildID)
		if err != nil {
			logger.Errorf("[SponsorBlock] Failed to get current state: %v", err)
			currentSponsorBlock = false
		}
		enabled = !currentSponsorBlock
	}

	if err := queue.SetSponsorBlock(i.GuildID, enabled); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Settings.SponsorBlockError, err)))
		return err
	}

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
		Title:       fmt.Sprintf(messages.T(i.GuildID).Settings.SponsorBlockTitle, statusEmoji, statusText),
		Description: fmt.Sprintf(messages.T(i.GuildID).Settings.SponsorBlockDesc, statusText),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T(i.GuildID).Settings.SponsorBlockWhatTitle,
				Value:  messages.T(i.GuildID).Settings.SponsorBlockWhatDesc,
				Inline: false,
			},
			{
				Name:   messages.T(i.GuildID).Settings.NoteTitle,
				Value:  messages.T(i.GuildID).Settings.SettingApplyNext,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}
