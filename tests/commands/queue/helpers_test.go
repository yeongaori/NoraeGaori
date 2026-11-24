package queue_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/database"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
)

func emptyQueueText(locale *messages.Locale) string {
	return locale.Descriptions.EmptyQueue
}

func advanceQueueBehindCache(t *testing.T) {
	t.Helper()

	if _, err := queue.GetQueue(commandtest.GuildID, false); err != nil {
		t.Fatalf("failed to update the queue state: %v", err)
	}
	if _, err := database.DB.Exec(`DELETE FROM songs WHERE guild_id = ? AND queue_position = 0`, commandtest.GuildID); err != nil {
		t.Fatalf("failed to update the queue state: %v", err)
	}
	if _, err := database.DB.Exec(`UPDATE songs SET queue_position = queue_position - 1 WHERE guild_id = ?`, commandtest.GuildID); err != nil {
		t.Fatalf("failed to update the queue state: %v", err)
	}
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
