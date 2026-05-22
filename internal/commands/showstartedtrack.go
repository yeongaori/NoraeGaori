package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func HandleShowStartedTrack(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var enabled bool
	if len(options) > 0 {
		mode := options[0].StringValue()
		enabled = mode == "on"
	} else {
		
		currentShowStartedTrack, err := queue.GetShowStartedTrack(i.GuildID)
		if err != nil {
			logger.Errorf("[ShowStartedTrack] Failed to get current state: %v", err)
			currentShowStartedTrack = true
		}
		enabled = !currentShowStartedTrack
	}

	if err := queue.SetShowStartedTrack(i.GuildID, enabled); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(messages.T(i.GuildID).Settings.ShowTrackError, err)))
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
		Title:       fmt.Sprintf(messages.T(i.GuildID).Settings.ShowTrackTitle, statusEmoji, statusText),
		Description: fmt.Sprintf(messages.T(i.GuildID).Settings.ShowTrackDesc, statusText),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T(i.GuildID).Settings.ShowTrackWhatTitle,
				Value:  messages.T(i.GuildID).Settings.ShowTrackWhatDesc,
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
