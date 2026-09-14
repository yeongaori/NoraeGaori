package automix

import (
	"fmt"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/discord"
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

func autoMixStyleFields(guildID string, categories []string) []*discordgo.MessageEmbedField {
	fields := make([]*discordgo.MessageEmbedField, 0, len(categories))
	for _, category := range categories {
		current, err := queue.GetAutoMixStyle(guildID, category)
		if err != nil {
			current = queue.AutoMixStyleAuto
		}
		values := transition.StyleValues(category)
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   category,
			Value:  fmt.Sprintf("**%s**\n%s", current, strings.Join(values, ", ")),
			Inline: false,
		})
	}
	return fields
}

func HandleAutoMixStyle(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	guildID := i.GuildID
	t := messages.T(guildID)
	options := i.ApplicationCommandData().Options

	category := autoMixStyleOptionValue(options, "category")
	style := autoMixStyleOptionValue(options, "style")

	if category == "" {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.AutoMixStyleTitle,
			Description: t.Settings.AutoMixStyleDesc,
			Fields:      autoMixStyleFields(guildID, queue.AutoMixStyleCategories()),
		})
		return nil
	}

	if transition.StyleValues(category) == nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.AutoMixStyleInvalidCategory, category, strings.Join(queue.AutoMixStyleCategories(), ", "))))
		return nil
	}

	if style == "" {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.AutoMixStyleTitle,
			Description: t.Settings.AutoMixStyleDesc,
			Fields:      autoMixStyleFields(guildID, []string{category}),
		})
		return nil
	}

	if !transition.ValidStyle(category, style) {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.AutoMixStyleInvalidValue, style, category,
				strings.Join(transition.StyleValues(category), ", "))))
		return nil
	}

	if err := queue.SetAutoMixStyle(guildID, category, style); err != nil {
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
