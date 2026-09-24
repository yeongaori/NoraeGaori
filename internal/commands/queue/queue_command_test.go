package queue

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func paginationText(page, totalPages, songs int) string {
	return fmt.Sprintf(messages.T(commandtest.GuildID).Footers.Pagination, page, totalPages, songs)
}

func TestQueueCommand(t *testing.T) {
	commandtest.Run(t, "queue", HandleQueue, []commandtest.Case{
		{
			Name:     "an empty queue",
			WantText: func(locale *messages.Locale) string { return locale.Descriptions.EmptyQueue },
		},
		{
			Name:     "a page past the end",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 25),
			Options:  []*discordgo.ApplicationCommandInteractionDataOption{discordtest.IntegerOption("page", 9)},
			WantText: func(*messages.Locale) string { return paginationText(3, 3, 25) },
		},
	})
}

func TestTurningAQueuePage(t *testing.T) {
	t.Run("redraws the requested page", func(t *testing.T) {
		fixture := commandtest.NewFixture(t, nil, queuetest.SongsBy(commandtest.CallerID, 25)...)

		turnQueuePage(fixture.Session, fixture.Component(queuePageRoute), []string{"2"})

		sent := fixture.Requests()
		commandtest.WantSingleResponse(t, sent, discordgo.InteractionResponseUpdateMessage)
		commandtest.WantReplyText(t, sent, paginationText(2, 3, 25))
	})
}

func TestOpeningTheMixPanelFromTheQueue(t *testing.T) {
	t.Run("opens the panel", func(t *testing.T) {
		fixture := commandtest.NewFixture(t, nil, queuetest.SongsBy(commandtest.CallerID, 2)...)

		openMixPanel(fixture.Session, fixture.Component(queueMixRoute), nil)

		reply := commandtest.WantSingleResponse(t, fixture.Requests(), discordgo.InteractionResponseChannelMessageWithSource)
		if discordtest.IsEphemeral(reply) {
			t.Error("the panel was sent privately")
		}
		if got := discordtest.JSONAt(t, reply.Body, "data", "components", 0, "components", 0, "custom_id"); got != "automix_pick:1" {
			t.Errorf("the panel picker routes to %v, want automix_pick:1", got)
		}
	})

	t.Run("reports an empty queue privately", func(t *testing.T) {
		fixture := commandtest.NewFixture(t, nil)

		openMixPanel(fixture.Session, fixture.Component(queueMixRoute), nil)

		sent := fixture.Requests()
		if reply := commandtest.WantSingleResponse(t, sent, discordgo.InteractionResponseChannelMessageWithSource); !discordtest.IsEphemeral(reply) {
			t.Error("the empty-queue notice was not private")
		}
		commandtest.WantReplyText(t, sent, messages.T(commandtest.GuildID).AutoMixPanel.EmptyTitle)
	})
}
