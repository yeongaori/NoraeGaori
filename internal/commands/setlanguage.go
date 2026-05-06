package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

// HandleSetLanguage sets, clears, or shows the per-guild language override.
func HandleSetLanguage(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	available := messages.AvailableLocales()
	sort.Strings(available)
	availableStr := strings.Join(available, ", ")
	defaultLang := config.GetConfig().Language

	t := messages.T(i.GuildID)

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

	if requested == "" {
		if err := queue.SetGuildLanguage(i.GuildID, ""); err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
				fmt.Sprintf(t.Settings.LanguageSaveFailed, err)))
			return err
		}
		// Re-resolve T so the response renders in the newly active language.
		t = messages.T(i.GuildID)
		embed := &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.LanguageResetTitle,
			Description: fmt.Sprintf(t.Settings.LanguageResetDesc, defaultLang),
		}
		RespondEmbed(s, i, embed)
		return nil
	}

	known := false
	for _, code := range available {
		if strings.EqualFold(code, requested) {
			requested = code
			known = true
			break
		}
	}
	if !known {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(t.Settings.LanguageUnknown, requested, availableStr)))
		return nil
	}

	if err := queue.SetGuildLanguage(i.GuildID, requested); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error,
			fmt.Sprintf(t.Settings.LanguageSaveFailed, err)))
		return err
	}

	// Re-resolve T so the response renders in the newly active language.
	t = messages.T(i.GuildID)
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.LanguageChangedTitle,
		Description: fmt.Sprintf(t.Settings.LanguageChangedDesc, requested),
	}
	RespondEmbed(s, i, embed)
	return nil
}
