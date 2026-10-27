package settings

import (
	"fmt"
	"noraegaori/internal/discord"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
	"noraegaori/internal/messages"
)

func showCurrentLanguage(s *discordgo.Session, i *discordgo.InteractionCreate, defaultLang, availableStr string) {
	t := messages.T(i.GuildID)
	current, err := guild.GetLanguage(i.GuildID)
	if err != nil {
		logger.Errorf("Failed to get current language: %v", err)
	}
	display := current
	if display == "" {
		display = defaultLang + " (default)"
	}
	embed := messages.CreateInfoEmbed(
		t.Settings.LanguageCurrentTitle,
		fmt.Sprintf(t.Settings.LanguageCurrentDesc, display, defaultLang, availableStr),
	)
	discord.RespondEmbed(s, i, embed)
}

func resetLanguage(s *discordgo.Session, i *discordgo.InteractionCreate, defaultLang string) error {
	t := messages.T(i.GuildID)
	if err := guild.SetLanguage(i.GuildID, ""); err != nil {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(t.Settings.LanguageSaveFailed, err))
		return err
	}

	t = messages.T(i.GuildID)
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.LanguageResetTitle,
		Description: fmt.Sprintf(t.Settings.LanguageResetDesc, defaultLang),
	}
	discord.RespondEmbed(s, i, embed)
	return nil
}

func matchLocale(available []string, requested string) (string, bool) {
	for _, code := range available {
		if strings.EqualFold(code, requested) {
			return code, true
		}
	}
	return requested, false
}

func HandleSetLanguage(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	available := messages.AvailableLocales()
	sort.Strings(available)
	availableStr := strings.Join(available, ", ")
	defaultLang := config.GetConfig().Language

	t := messages.T(i.GuildID)

	if len(options) == 0 {
		showCurrentLanguage(s, i, defaultLang, availableStr)
		return nil
	}

	requested := strings.TrimSpace(options[0].StringValue())

	if requested == "" {
		return resetLanguage(s, i, defaultLang)
	}

	requested, known := matchLocale(available, requested)
	if !known {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(t.Settings.LanguageUnknown, requested, availableStr))
		return nil
	}

	if err := guild.SetLanguage(i.GuildID, requested); err != nil {
		discord.RespondErrorEmbed(s, i, fmt.Sprintf(t.Settings.LanguageSaveFailed, err))
		return err
	}

	t = messages.T(i.GuildID)
	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.LanguageChangedTitle,
		Description: fmt.Sprintf(t.Settings.LanguageChangedDesc, requested),
	}
	discord.RespondEmbed(s, i, embed)
	return nil
}
