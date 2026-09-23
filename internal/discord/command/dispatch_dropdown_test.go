package command

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
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

func TestHandleInteractionRoutesComponentsAndModalsByPrefix(t *testing.T) {
	called := registerProbeCommand(t, "dispatch_probe", false)

	var routed []string
	discord.RegisterComponentRoute("dispatch_probe", func(_ *discordgo.Session, _ *discordgo.InteractionCreate, arguments []string) {
		routed = append(routed, arguments...)
	})

	for _, ic := range []*discordgo.InteractionCreate{
		{Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionMessageComponent,
			GuildID: "guild",
			Data:    discordgo.MessageComponentInteractionData{CustomID: "dispatch_probe:1"},
		}},
		{Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionModalSubmit,
			GuildID: "guild",
			Data:    discordgo.ModalSubmitInteractionData{CustomID: "dispatch_probe:2"},
		}},
		{Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionModalSubmit,
			GuildID: "guild",
			Data:    discordgo.ModalSubmitInteractionData{CustomID: "dropdown_menu_probe"},
		}},
	} {
		HandleInteraction(nil, ic)
	}

	if len(routed) != 2 || routed[0] != "1" || routed[1] != "2" {
		t.Errorf("routed arguments = %v, want the component then the modal", routed)
	}
	if *called {
		t.Error("a routed interaction ran a command handler")
	}
}
