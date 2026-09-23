package queue

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/discordtest"
)

func TestRegisterAddsTheQueueCommandsAndRoutes(t *testing.T) {
	Register(func(string) messages.CommandStrings { return messages.CommandStrings{} })

	commandtest.WantRegistered(t, map[string]commandtest.Registration{
		"queue":     {Handler: HandleQueue},
		"remove":    {Handler: HandleRemove},
		"swap":      {Handler: HandleSwap},
		"skipto":    {Handler: HandleSkipTo},
		"movetrack": {Handler: HandleMoveTrack, IsAdminOnly: true},
	})

	page := discordtest.ComponentInteraction(commandtest.GuildID, queuePageRoute+":not-a-page", nil)
	mix := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:    discordgo.InteractionModalSubmit,
		GuildID: commandtest.GuildID,
		Data:    discordgo.ModalSubmitInteractionData{CustomID: queueMixRoute},
	}}
	for _, ic := range []*discordgo.InteractionCreate{page, mix} {
		if !discord.HandleComponentRoute(nil, ic) {
			t.Errorf("interaction type %v was not routed to the queue", ic.Type)
		}
	}
}
