package commands

import (
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func HandleVolume(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	if len(options) == 0 {
		volume, err := queue.GetVolume(i.GuildID)
		if err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.VolumeQueryFailed, err)))
			return err
		}
		RespondEmbed(s, i, messages.CreateInfoEmbed(messages.T(i.GuildID).Music.CurrentVolumeTitle, fmt.Sprintf(messages.T(i.GuildID).Music.CurrentVolumeDesc, volume)))
		return nil
	}

	// Value is float64 from slash commands, string from text commands.
	var volume float64
	switch v := options[0].Value.(type) {
	case float64:
		volume = v
	case string:
		var err error
		volume, err = strconv.ParseFloat(v, 64)
		if err != nil {
			RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.VolumeNotNumber))
			return nil
		}
	default:
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.VolumeNotNumber))
		return nil
	}

	if volume < 0 || volume > 1000 {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, messages.T(i.GuildID).Music.VolumeOutOfRange))
		return nil
	}

	if err := player.SetVolume(i.GuildID, volume); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.VolumeSetFailed, err)))
		return err
	}

	RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Music.VolumeSetTitle, fmt.Sprintf(messages.T(i.GuildID).Music.VolumeSetDesc, volume)))
	return nil
}
