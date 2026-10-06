package automix

import (
	"fmt"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
	"slices"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func optionByName(options []*discordgo.ApplicationCommandInteractionDataOption, name string) *discordgo.ApplicationCommandInteractionDataOption {
	for _, opt := range options {
		if opt.Name == name {
			return opt
		}
	}
	return nil
}

func autoMixStyleOptionValue(options []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	opt := optionByName(options, name)
	if opt == nil {
		return ""
	}
	value, ok := opt.Value.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(value))
}

var commandCategories = slices.Concat(transition.StyleCategories(), transition.ShortcutCategories())

func joinCategories(categories []transition.Category) string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, string(category))
	}
	return strings.Join(names, ", ")
}

func currentGuildStyle(overrides map[string]string, category transition.Category) string {
	targets := transition.ShortcutTargets(category)
	if len(targets) == 0 {
		targets = []transition.Category{category}
	}
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		value, ok := overrides[string(target)]
		if !ok {
			value = transition.StyleAuto
		}
		values = append(values, value)
	}
	return strings.Join(values, " / ")
}

func autoMixStyleFields(guildID string, categories []transition.Category) []*discordgo.MessageEmbedField {
	overrides, _ := queue.GetAutoMixOverrides(guildID)
	fields := make([]*discordgo.MessageEmbedField, 0, len(categories))
	for _, category := range categories {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   string(category),
			Value:  fmt.Sprintf("**%s**\n%s", currentGuildStyle(overrides, category), strings.Join(transition.StyleValues(category), ", ")),
			Inline: false,
		})
	}
	return fields
}

func HandleAutoMixStyle(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	options := i.ApplicationCommandData().Options

	categoryName := autoMixStyleOptionValue(options, "category")
	style := autoMixStyleOptionValue(options, "style")

	if categoryName == "" {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.AutoMixStyleTitle,
			Description: t.Settings.AutoMixStyleDesc,
			Fields:      autoMixStyleFields(guildID, transition.StyleCategories()),
		})
		return nil
	}

	category, _ := transition.ParseCategory(categoryName)
	if !slices.Contains(commandCategories, category) {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.AutoMixStyleInvalidCategory, categoryName, joinCategories(commandCategories))))
		return nil
	}

	if style == "" {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.AutoMixStyleTitle,
			Description: t.Settings.AutoMixStyleDesc,
			Fields:      autoMixStyleFields(guildID, []transition.Category{category}),
		})
		return nil
	}

	if !transition.ValidStyle(category, style) {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.AutoMixStyleInvalidValue, style, category,
				strings.Join(transition.StyleValues(category), ", "))))
		return nil
	}

	if err := queue.SetAutoMixOverrides(guildID, transition.ExpandLegacy(category, style)); err != nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error, fmt.Sprintf(t.Settings.AutoMixStyleError, err)))
		return err
	}

	discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.AutoMixStyleTitle,
		Description: fmt.Sprintf(t.Settings.AutoMixStyleChanged, category, style),
		Fields: []*discordgo.MessageEmbedField{
			{Name: t.Settings.AutoMixStyleWhatTitle, Value: t.Settings.AutoMixStyleWhatDesc, Inline: false},
		},
	})
	return nil
}
