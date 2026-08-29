package settings

import (
	"fmt"
	"noraegaori/internal/discord"
	"strings"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/config"
	"noraegaori/internal/guild"
	"noraegaori/internal/messages"
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
		if err := guild.SetPrefix(i.GuildID, ""); err != nil {
			discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
				fmt.Sprintf(t.Settings.PrefixError, err)))
			return err
		}
		embed := &discordgo.MessageEmbed{
			Color:       messages.ColorSuccess,
			Title:       t.Settings.PrefixResetTitle,
			Description: fmt.Sprintf(t.Settings.PrefixResetDesc, defaultPrefix),
		}
		discord.RespondEmbed(s, i, embed)
		return nil
	}

	if len([]rune(requested)) > maxPrefixLength {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
			t.Settings.PrefixTooLong))
		return nil
	}

	if err := guild.SetPrefix(i.GuildID, requested); err != nil {
		discord.RespondEmbed(s, i, messages.CreateErrorEmbed(t.Titles.Error,
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

	discord.RespondEmbed(s, i, embed)
	return nil
}
