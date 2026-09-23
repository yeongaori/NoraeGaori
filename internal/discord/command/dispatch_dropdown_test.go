package command

import (
	"net/http"
	"slices"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/testutil/discordtest"
)

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

func TestHandleInteractionAppliesADropdownPick(t *testing.T) {
	var picked []string
	discord.RegisterDropdownMenu("dispatch_menu", func(string) discord.DropdownMenu {
		return discord.DropdownMenu{
			Label:   "Probe",
			Options: []discordgo.SelectMenuOption{{Label: "On", Value: "on"}},
			Apply: func(value string) (*discordgo.MessageEmbed, error) {
				picked = append(picked, value)
				return nil, nil
			},
		}
	})
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	HandleInteraction(session, discordtest.ComponentInteraction("guild", "dropdown_menu_dispatch_menu", nil, "on"))

	if !slices.Equal(picked, []string{"on"}) {
		t.Errorf("picked %v, want the one pick applied", picked)
	}
	if sent := requests(); len(sent) != 1 || discordtest.ResponseType(&sent[0]) != discordgo.InteractionResponseUpdateMessage {
		t.Errorf("sent %v, want the menu redrawn in place", sent)
	}
}
