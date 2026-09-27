package command

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/tests/testutil/discordtest"
)

func recordBeforeCommand(t *testing.T) *[]string {
	t.Helper()

	previous := beforeCommand.Load()
	t.Cleanup(func() { beforeCommand.Store(previous) })

	var guildIDs []string
	SetBeforeCommand(func(guildID string) { guildIDs = append(guildIDs, guildID) })
	return &guildIDs
}

func TestBeforeCommandRunsAheadOfTheSlashHandler(t *testing.T) {
	guildIDs := recordBeforeCommand(t)
	hookRanFirst := false
	registerProbe(t, "probe", false, func(*discordgo.Session, *discordgo.InteractionCreate) error {
		hookRanFirst = len(*guildIDs) == 1
		return nil
	})
	session := &discordgo.Session{State: discordgo.NewState()}
	member := &discordgo.Member{GuildID: "guild", User: &discordgo.User{ID: "user"}}

	HandleInteraction(session, probeInteraction("probe", member))

	if !hookRanFirst {
		t.Error("the handler ran before the before-command hook")
	}
	if len(*guildIDs) != 1 || (*guildIDs)[0] != "guild" {
		t.Errorf("hook calls = %v, want one for guild", *guildIDs)
	}
}

func TestBeforeCommandRunsForComponentsButNotAutocomplete(t *testing.T) {
	guildIDs := recordBeforeCommand(t)
	session, _ := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	HandleInteraction(session, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:      "1",
		Token:   "token",
		GuildID: "guild",
		Type:    discordgo.InteractionApplicationCommandAutocomplete,
		Data:    discordgo.ApplicationCommandInteractionData{Name: "no-such-command"},
	}})
	if len(*guildIDs) != 0 {
		t.Fatalf("hook calls = %v after autocomplete, want none", *guildIDs)
	}

	HandleInteraction(session, discordtest.ComponentInteraction("guild", "no-such-component", discordtest.Member("guild", "user")))
	if len(*guildIDs) != 1 || (*guildIDs)[0] != "guild" {
		t.Errorf("hook calls = %v after a component, want one for guild", *guildIDs)
	}
}

func TestBeforeCommandRunsForTextCommandsOnly(t *testing.T) {
	guildIDs := recordBeforeCommand(t)
	session, _ := dispatchSession(t)
	registerProbeCommand(t, "probe", false)
	registerProbeAlias(t, "probe", "probe")

	HandleMessage(session, textMessage("just chatting"))
	if len(*guildIDs) != 0 {
		t.Fatalf("hook calls = %v for a plain message, want none", *guildIDs)
	}

	HandleMessage(session, textMessage("!probe"))
	if len(*guildIDs) != 1 || (*guildIDs)[0] != dispatchGuildID {
		t.Errorf("hook calls = %v for a text command, want one for %s", *guildIDs, dispatchGuildID)
	}
}
