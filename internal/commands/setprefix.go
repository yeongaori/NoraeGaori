package commands

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func HandleSetPrefix(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	defaultPrefix := config.GetConfig().Prefix
	t := messages.T(i.GuildID)

	requested := ""
	if len(options) > 0 {
		requested = strings.TrimSpace(options[0].StringValue())
	}

	if requested == "" {
		if err := queue.SetGuildPrefix(i.GuildID, ""); err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
				fmt.Sprintf(t.Settings.PrefixError, err)))
			return err
		}
		embed := &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.PrefixResetTitle,
			Description: fmt.Sprintf(t.Settings.PrefixResetDesc, defaultPrefix),
		}
		RespondEmbed(s, i, embed)
		return nil
	}

	if len(requested) > 5 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			t.Settings.PrefixTooLong))
		return nil
	}

	if err := queue.SetGuildPrefix(i.GuildID, requested); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			fmt.Sprintf(t.Settings.PrefixError, err)))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Color:       messages.ColorSuccess,
		Title:       t.Settings.PrefixChangedTitle,
		Description: fmt.Sprintf(t.Settings.PrefixChangedDesc, requested),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   t.Settings.PrefixExampleTitle,
				Value:  fmt.Sprintf(t.Settings.PrefixExampleValue, requested, requested, requested),
				Inline: false,
			},
		},
	}

	RespondEmbed(s, i, embed)
	return nil
}
