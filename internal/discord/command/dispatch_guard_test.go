package command

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func registerProbeCommand(t *testing.T, name string, adminOnly bool) *bool {
	t.Helper()

	return registerProbe(t, name, adminOnly, func(*discordgo.Session, *discordgo.InteractionCreate) error {
		return nil
	})
}

func registerProbe(t *testing.T, name string, adminOnly bool, handler func(*discordgo.Session, *discordgo.InteractionCreate) error) *bool {
	t.Helper()

	called := false
	registerTestCommand(t, &Command{
		Name:      name,
		AdminOnly: adminOnly,
		Handler: func(s *discordgo.Session, i *discordgo.InteractionCreate) error {
			called = true
			return handler(s, i)
		},
	})
	return &called
}

func registerTestCommand(t *testing.T, cmd *Command) {
	t.Helper()

	previous := commands.Load()
	rebuilt := make(map[string]*Command, len(*previous)+1)
	for existing, registered := range *previous {
		rebuilt[existing] = registered
	}
	rebuilt[cmd.Name] = cmd
	commands.Store(&rebuilt)

	t.Cleanup(func() {
		commands.Store(previous)
	})
}

func useRegistry(t *testing.T, cmds ...*Command) {
	t.Helper()

	previous := commands.Load()
	replaced := make(map[string]*Command, len(cmds))
	for _, cmd := range cmds {
		replaced[cmd.Name] = cmd
	}
	commands.Store(&replaced)

	t.Cleanup(func() {
		commands.Store(previous)
	})
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

func TestHandleInteractionRunsTheHandlerForAGuildMember(t *testing.T) {
	called := registerProbeCommand(t, "probe", false)
	session := &discordgo.Session{State: discordgo.NewState()}
	member := &discordgo.Member{GuildID: "guild", User: &discordgo.User{ID: "user"}}

	HandleInteraction(session, probeInteraction("probe", member))

	if !*called {
		t.Error("the handler did not run for a valid guild member")
	}
}
