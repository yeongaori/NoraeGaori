package commands

import (
	"fmt"
	"sort"
	"strings"

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
			fmt.Sprintf(messages.T(i.GuildID).Settings.NormalizationError, err)))
		return err
	}

	// Restart FFmpeg to apply normalization change immediately
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

// HandleSetLanguage handles the setlanguage command — admin-only, sets the
// per-guild language override stored in guild_settings.language.
func HandleSetLanguage(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	available := messages.AvailableLocales()
	sort.Strings(available)
	availableStr := strings.Join(available, ", ")
	defaultLang := config.GetConfig().Language

	t := messages.T(i.GuildID)

	// No args → show current setting.
	if len(options) == 0 {
		current, err := queue.GetGuildLanguage(i.GuildID)
		if err != nil {
			logger.Errorf("[SetLanguage] Failed to get current language: %v", err)
		}
		display := current
		if display == "" {
			display = defaultLang + " (default)"
		}
		embed := messages.CreateInfoEmbed(
			t.Settings.LanguageCurrentTitle,
			fmt.Sprintf(t.Settings.LanguageCurrentDesc, display, defaultLang, availableStr),
		)
		RespondEmbed(s, i, embed)
		return nil
	}

	requested := strings.TrimSpace(options[0].StringValue())

	// Empty value → clear override.
	if requested == "" {
		if err := queue.SetGuildLanguage(i.GuildID, ""); err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
				fmt.Sprintf(t.Settings.LanguageSaveFailed, err)))
			return err
		}
		// Re-resolve T after the change so the response renders in the new language.
		t = messages.T(i.GuildID)
		embed := &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.LanguageResetTitle,
			Description: fmt.Sprintf(t.Settings.LanguageResetDesc, defaultLang),
		}
		RespondEmbed(s, i, embed)
		return nil
	}

	// Validate against available locales.
	known := false
	for _, code := range available {
		if strings.EqualFold(code, requested) {
			requested = code
			known = true
			break
		}
	}
	if !known {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(t.Settings.LanguageUnknown, requested, availableStr)))
		return nil
	}

	if err := queue.SetGuildLanguage(i.GuildID, requested); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(t.Settings.LanguageSaveFailed, err)))
		return err
	}

	// Re-resolve T after the change so the success embed renders in the new language.
	t = messages.T(i.GuildID)
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.LanguageChangedTitle,
		Description: fmt.Sprintf(t.Settings.LanguageChangedDesc, requested),
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
			messages.T(i.GuildID).Settings.CurrentPrefixTitle,
			fmt.Sprintf(messages.T(i.GuildID).Settings.CurrentPrefixDesc, cfg.Prefix, cfg.Prefix),
		)
		RespondEmbed(s, i, embed)
		return nil
	}

	newPrefix := options[0].StringValue()

	// Validate prefix
	if len(newPrefix) == 0 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T(i.GuildID).Settings.PrefixEmpty))
		return nil
	}

	if len(newPrefix) > 5 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			messages.T(i.GuildID).Settings.PrefixTooLong))
		return nil
	}

	// Update prefix
	if err := config.SetPrefix(newPrefix); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.TitleError,
			fmt.Sprintf(messages.T(i.GuildID).Settings.PrefixError, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       messages.T(i.GuildID).Settings.PrefixChangedTitle,
		Description: fmt.Sprintf(messages.T(i.GuildID).Settings.PrefixChangedDesc, newPrefix),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   messages.T(i.GuildID).Settings.PrefixExampleTitle,
				Value:  fmt.Sprintf(messages.T(i.GuildID).Settings.PrefixExampleValue, newPrefix, newPrefix, newPrefix),
				Inline: false,
			},
			{
				Name:   messages.T(i.GuildID).Settings.NoteTitle,
				Value:  messages.T(i.GuildID).Settings.PrefixSlashNote,
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}

