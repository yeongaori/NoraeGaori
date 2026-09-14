package command

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestHandleInteractionKeepsComponentsAwayFromCommands(t *testing.T) {
	called := registerProbeCommand(t, "dropdown_menu_probe", false)

	for _, customID := range []string{"dropdown_menu_probe", "dropdown_menu_unregistered", "settings_pick_token"} {
		HandleInteraction(nil, &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				Type:    discordgo.InteractionMessageComponent,
				GuildID: "guild",
				Data:    discordgo.MessageComponentInteractionData{CustomID: customID, Values: []string{"on"}},
			},
		})
	}

	if *called {
		t.Error("a component interaction ran a command handler")
	}
}
