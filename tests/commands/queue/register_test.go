package queue_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	queuecommand "noraegaori/internal/commands/queue"
	"noraegaori/internal/discord"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
)

func TestRegisterAddsTheQueueCommandsAndRoutes(t *testing.T) {
	queuecommand.Register(func(string) messages.CommandStrings { return messages.CommandStrings{} })

	commandtest.WantRegistered(t, map[string]commandtest.Registration{
		"queue":     {Handler: queuecommand.HandleQueue},
		"remove":    {Handler: queuecommand.HandleRemove},
		"swap":      {Handler: queuecommand.HandleSwap},
		"skipto":    {Handler: queuecommand.HandleSkipTo},
		"movetrack": {Handler: queuecommand.HandleMoveTrack, IsAdminOnly: true},
	})

	page := discordtest.ComponentInteraction(commandtest.GuildID, queuecommand.HookQueuePageRoute+":not-a-page", nil)
	mix := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:    discordgo.InteractionModalSubmit,
		GuildID: commandtest.GuildID,
		Data:    discordgo.ModalSubmitInteractionData{CustomID: queuecommand.HookQueueMixRoute},
	}}
	for _, ic := range []*discordgo.InteractionCreate{page, mix} {
		if !discord.HandleComponentRoute(nil, ic) {
			t.Errorf("interaction type %v was not routed to the queue", ic.Type)
		}
	}
}
