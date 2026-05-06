package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/pkg/logger"
)

func HandleRepeat(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	options := i.ApplicationCommandData().Options

	var mode int
	if len(options) > 0 {
		arg := options[0].StringValue()
		switch arg {
		case "on", "all":
			mode = queue.RepeatAll
		case "single":
			mode = queue.RepeatSingle
		default:
			mode = queue.RepeatOff
		}
	} else {
		// No arg → cycle Off → All → Single → Off.
		currentMode, err := queue.GetRepeatMode(i.GuildID)
		if err != nil {
			logger.Errorf("[Repeat] Failed to get current repeat mode: %v", err)
			currentMode = queue.RepeatOff
		}
		switch currentMode {
		case queue.RepeatOff:
			mode = queue.RepeatAll
		case queue.RepeatAll:
			mode = queue.RepeatSingle
		case queue.RepeatSingle:
			mode = queue.RepeatOff
		}
	}

	if err := queue.SetRepeatMode(i.GuildID, mode); err != nil {
		RespondEmbed(s, i, messages.CreateErrorEmbed(messages.T(i.GuildID).Titles.Error, fmt.Sprintf(messages.T(i.GuildID).Music.RepeatSetFailed, err)))
		return err
	}

	switch mode {
	case queue.RepeatAll:
		RespondEmbed(s, i, messages.CreateSuccessEmbed(messages.T(i.GuildID).Titles.RepeatAll, messages.T(i.GuildID).Descriptions.RepeatAll))
	case queue.RepeatSingle:
		RespondEmbed(s, i, messages.CreateInfoEmbed(messages.T(i.GuildID).Titles.RepeatSingle, messages.T(i.GuildID).Descriptions.RepeatSingle))
	default:
		RespondEmbed(s, i, messages.CreateWarningEmbed(messages.T(i.GuildID).Titles.RepeatOff, messages.T(i.GuildID).Descriptions.RepeatOff))
	}
	return nil
}
