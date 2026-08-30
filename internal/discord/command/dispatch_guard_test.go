package command

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func registerProbeCommand(t *testing.T, name string, adminOnly bool) *bool {
	t.Helper()

	called := false

	commandsMu.Lock()
	previous := commands
	commands = map[string]*Command{}
	for existing, cmd := range previous {
		commands[existing] = cmd
	}
	commandsMu.Unlock()

	t.Cleanup(func() {
		commandsMu.Lock()
		commands = previous
		commandsMu.Unlock()
	})

	RegisterCommand(&Command{
		Name:      name,
		AdminOnly: adminOnly,
		Handler: func(*discordgo.Session, *discordgo.InteractionCreate) error {
			called = true
			return nil
		},
	})

	return &called
}

func probeInteraction(name string, member *discordgo.Member) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: "guild",
			Member:  member,
			Token:   "message_probe_channel",
			Data: discordgo.ApplicationCommandInteractionData{
				Name: name,
			},
		},
	}
}

func TestHandleInteractionSkipsTheHandlerWithoutAMember(t *testing.T) {
	called := registerProbeCommand(t, "probe", false)
	session := &discordgo.Session{State: discordgo.NewState()}

	HandleInteraction(session, probeInteraction("probe", nil))

	if *called {
		t.Error("the handler ran for an interaction that carried no guild member")
	}
}

func TestHandleInteractionSkipsTheHandlerWhenTheMemberHasNoUser(t *testing.T) {
	called := registerProbeCommand(t, "probe", false)
	session := &discordgo.Session{State: discordgo.NewState()}

	HandleInteraction(session, probeInteraction("probe", &discordgo.Member{}))

	if *called {
		t.Error("the handler ran for a member that carried no user")
	}
}

func TestHandleInteractionRunsTheHandlerForAGuildMember(t *testing.T) {
	called := registerProbeCommand(t, "probe", false)
	session := &discordgo.Session{State: discordgo.NewState()}
	member := &discordgo.Member{GuildID: "guild", User: &discordgo.User{ID: "user"}}

	HandleInteraction(session, probeInteraction("probe", member))

	if !*called {
		t.Error("the handler did not run for a valid guild member")
	}
}

func TestHandleInteractionSkipsAdminCommandsForNonAdmins(t *testing.T) {
	called := registerProbeCommand(t, "probeadmin", true)
	session := &discordgo.Session{State: discordgo.NewState()}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild"}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	member := &discordgo.Member{GuildID: "guild", User: &discordgo.User{ID: "user"}}

	HandleInteraction(session, probeInteraction("probeadmin", member))

	if *called {
		t.Error("an admin-only handler ran for a member without administrator permission")
	}
}
