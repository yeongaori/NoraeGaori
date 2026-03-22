package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

// HandleSponsorBlock handles the sponsorblock command
func HandleSponsorBlock(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var enabled bool
	if len(options) > 0 {
		mode := options[0].StringValue()
		enabled = mode == "on"
	} else {
		// Toggle current state - read directly from database
		currentSponsorBlock, err := queue.GetSponsorBlock(i.GuildID)
		if err != nil {
			logger.Errorf("[SponsorBlock] Failed to get current state: %v", err)
			currentSponsorBlock = false // default to false on error
		}
		enabled = !currentSponsorBlock
	}

	if err := queue.SetSponsorBlock(i.GuildID, enabled); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Settings.SponsorBlockError, err)))
		return err
	}

	statusText := messages.T().Settings.StatusOff
	statusEmoji := ""
	color := messages.ColorError
	if enabled {
		statusText = messages.T().Settings.StatusOn
		statusEmoji = ""
		color = messages.ColorSuccess
	}

	embed := &discordgo.MessageEmbed{
		Color:       color,
		Title:       fmt.Sprintf(messages.T().Settings.SponsorBlockTitle, statusEmoji, statusText),
		Description: fmt.Sprintf(messages.T().Settings.SponsorBlockDesc, statusText),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T().Settings.SponsorBlockWhatTitle,
				Value:  messages.T().Settings.SponsorBlockWhatDesc,
				Inline: false,
			},
			{
				Name:   messages.T().Settings.NoteTitle,
				Value:  messages.T().Settings.SettingApplyNext,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}

// HandleShowStartedTrack handles the showstartedtrack command
func HandleShowStartedTrack(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var enabled bool
	if len(options) > 0 {
		mode := options[0].StringValue()
		enabled = mode == "on"
	} else {
		// Toggle current state - read directly from database
		currentShowStartedTrack, err := queue.GetShowStartedTrack(i.GuildID)
		if err != nil {
			logger.Errorf("[ShowStartedTrack] Failed to get current state: %v", err)
			currentShowStartedTrack = true // default to true on error (enabled by default)
		}
		enabled = !currentShowStartedTrack
	}

	if err := queue.SetShowStartedTrack(i.GuildID, enabled); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Settings.ShowTrackError, err)))
		return err
	}

	statusText := messages.T().Settings.StatusOff
	statusEmoji := ""
	color := messages.ColorError
	if enabled {
		statusText = messages.T().Settings.StatusOn
		statusEmoji = ""
		color = messages.ColorSuccess
	}

	embed := &discordgo.MessageEmbed{
		Color:       color,
		Title:       fmt.Sprintf(messages.T().Settings.ShowTrackTitle, statusEmoji, statusText),
		Description: fmt.Sprintf(messages.T().Settings.ShowTrackDesc, statusText),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T().Settings.ShowTrackWhatTitle,
				Value:  messages.T().Settings.ShowTrackWhatDesc,
				Inline: false,
			},
			{
				Name:   messages.T().Settings.NoteTitle,
				Value:  messages.T().Settings.SettingApplyNext,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}

// HandleNormalization handles the normalization command
func HandleNormalization(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var enabled bool
	if len(options) > 0 {
		mode := options[0].StringValue()
		enabled = mode == "on"
	} else {
		// Toggle current state - read directly from database
		currentNormalization, err := queue.GetNormalization(i.GuildID)
		if err != nil {
			logger.Errorf("[Normalization] Failed to get current state: %v", err)
			currentNormalization = false // default to false on error
		}
		enabled = !currentNormalization
	}

	if err := queue.SetNormalization(i.GuildID, enabled); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Settings.NormalizationError, err)))
		return err
	}

	// Restart FFmpeg to apply normalization change immediately
	player.RestartForNormalization(i.GuildID)

	statusText := messages.T().Settings.StatusOff
	statusEmoji := ""
	color := messages.ColorError
	if enabled {
		statusText = messages.T().Settings.StatusOn
		statusEmoji = ""
		color = messages.ColorSuccess
	}

	embed := &discordgo.MessageEmbed{
		Color:       color,
		Title:       fmt.Sprintf(messages.T().Settings.NormalizationTitle, statusEmoji, statusText),
		Description: fmt.Sprintf(messages.T().Settings.NormalizationDesc, statusText),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T().Settings.NormalizationWhatTitle,
				Value:  messages.T().Settings.NormalizationWhatDesc,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}

// HandleSetPrefix handles the setprefix command
func HandleSetPrefix(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	if len(options) == 0 {
		// Show current prefix
		cfg := config.GetConfig()
		embed := messages.CreateInfoEmbed(
			messages.T().Settings.CurrentPrefixTitle,
			fmt.Sprintf(messages.T().Settings.CurrentPrefixDesc, cfg.Prefix, cfg.Prefix),
		)
		RespondEmbed(s, i, embed)
		return nil
	}

	newPrefix := options[0].StringValue()

	// Validate prefix
	if len(newPrefix) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Settings.PrefixEmpty))
		return nil
	}

	if len(newPrefix) > 5 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T().Settings.PrefixTooLong))
		return nil
	}

	// Update prefix
	if err := config.SetPrefix(newPrefix); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T().Settings.PrefixError, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T().Settings.PrefixChangedTitle,
		Description: fmt.Sprintf(messages.T().Settings.PrefixChangedDesc, newPrefix),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T().Settings.PrefixExampleTitle,
				Value:  fmt.Sprintf(messages.T().Settings.PrefixExampleValue, newPrefix, newPrefix, newPrefix),
				Inline: false,
			},
			{
				Name:   messages.T().Settings.NoteTitle,
				Value:  messages.T().Settings.PrefixSlashNote,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}

