package queue

import (
	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/discordtest"
)

func emptyQueueText(locale *messages.Locale) string {
	return locale.Descriptions.EmptyQueue
}

func moveOptions(from, to float64) []*discordgo.ApplicationCommandInteractionDataOption {
	return integerPair("from", "to", from, to)
}

func swapOptions(first, second float64) []*discordgo.ApplicationCommandInteractionDataOption {
	return integerPair("position1", "position2", first, second)
}

func integerPair(firstName, secondName string, first, second float64) []*discordgo.ApplicationCommandInteractionDataOption {
	return []*discordgo.ApplicationCommandInteractionDataOption{
		discordtest.IntegerOption(firstName, first),
		discordtest.IntegerOption(secondName, second),
	}
}

func skipToOption(position float64) []*discordgo.ApplicationCommandInteractionDataOption {
	return []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("position", position)}
}

func removeOption(position string) []*discordgo.ApplicationCommandInteractionDataOption {
	return []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("position", position)}
}
