package command

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/database"
	"noraegaori/internal/discord"
	"noraegaori/internal/guild"
	"noraegaori/internal/testutil/configtest"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/discordtest"
)

const (
	dispatchGuildID    = "dispatch-guild"
	dispatchAdminRole  = "dispatch-admin"
	dispatchMemberRole = "dispatch-member"
)

func registerProbeAlias(t *testing.T, alias, name string) {
	t.Helper()

	previous := aliases.Load()
	t.Cleanup(func() { aliases.Store(previous) })
	RegisterAlias(alias, name)
}

func dispatchSession(t *testing.T) (*discordgo.Session, func() []discordtest.Request) {
	t.Helper()

	dbtest.Setup(t)
	configtest.Setup(t)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	err := session.State.GuildAdd(&discordgo.Guild{
		ID: dispatchGuildID,
		Roles: []*discordgo.Role{
			{ID: dispatchGuildID},
			{ID: dispatchAdminRole, Permissions: discordgo.PermissionAdministrator},
			{ID: dispatchMemberRole, Permissions: discordgo.PermissionSendMessages},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	return session, requests
}

func textMessage(content string, roles ...string) *discordgo.MessageCreate {
	return &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "111",
			ChannelID: "222",
			GuildID:   dispatchGuildID,
			Content:   content,
			Author:    &discordgo.User{ID: "author"},
			Member:    &discordgo.Member{Roles: roles},
		},
	}
}

func failingHandler(*discordgo.Session, *discordgo.InteractionCreate) error {
	return errors.New("probe failed")
}

func TestHandleInteractionAnswersAutocompleteForUnknownCommands(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	HandleInteraction(session, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:    "1",
			Token: "token",
			Type:  discordgo.InteractionApplicationCommandAutocomplete,
			Data:  discordgo.ApplicationCommandInteractionData{Name: "no-such-command"},
		},
	})

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want one autocomplete reply", len(sent))
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "type"); got != float64(discordgo.InteractionApplicationCommandAutocompleteResult) {
		t.Errorf("reply type = %v, want an autocomplete result", got)
	}
}

func TestHandleInteractionIgnoresOtherInteractionTypes(t *testing.T) {
	called := registerProbeCommand(t, "probe", false)

	HandleInteraction(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{Type: discordgo.InteractionModalSubmit},
	})

	if *called {
		t.Error("a modal submission ran a command handler")
	}
}

func TestHandleInteractionRejectsUnknownCommands(t *testing.T) {
	called := registerProbeCommand(t, "probe", false)
	member := &discordgo.Member{GuildID: "guild", User: &discordgo.User{ID: "user"}}

	HandleInteraction(nil, probeInteraction("ghost", member))

	if *called {
		t.Error("an unknown command ran another command's handler")
	}
}

func TestHandleInteractionReportsAFailingHandler(t *testing.T) {
	called := registerProbe(t, "probefail", false, failingHandler)
	member := &discordgo.Member{GuildID: "guild", User: &discordgo.User{ID: "user"}}

	HandleInteraction(nil, probeInteraction("probefail", member))

	if !*called {
		t.Error("the failing handler did not run")
	}
}

func TestHandleMessageIgnoresMessagesThatAreNotCommands(t *testing.T) {
	session, requests := dispatchSession(t)
	called := registerProbeCommand(t, "probe", false)
	registerProbeAlias(t, "probe", "probe")
	registerProbeAlias(t, "ghost", "ghost-command")

	fromBot := textMessage("!probe")
	fromBot.Author.Bot = true

	for _, message := range []*discordgo.MessageCreate{
		fromBot,
		textMessage("probe"),
		textMessage("!"),
		textMessage("!nope"),
		textMessage("!ghost"),
	} {
		HandleMessage(session, message)
	}

	if *called {
		t.Error("a message that is not a command ran the handler")
	}
	if sent := requests(); len(sent) != 0 {
		t.Errorf("sent %v, want no requests", sent)
	}
}

func TestHandleMessageUsesTheServersOwnPrefix(t *testing.T) {
	session, _ := dispatchSession(t)
	called := registerProbeCommand(t, "probe", false)
	registerProbeAlias(t, "probe", "probe")
	t.Cleanup(func() { guild.InvalidateCaches(dispatchGuildID) })

	if err := guild.SetPrefix(dispatchGuildID, "?"); err != nil {
		t.Fatalf("failed to set the server prefix: %v", err)
	}

	HandleMessage(session, textMessage("?probe"))

	if !*called {
		t.Error("a command with the server's own prefix did not run")
	}
}

func TestHandleMessageFallsBackToTheConfiguredPrefixWhenTheServerPrefixCannotBeRead(t *testing.T) {
	session, _ := dispatchSession(t)
	called := registerProbeCommand(t, "probe", false)
	registerProbeAlias(t, "probe", "probe")
	guild.InvalidateCaches(dispatchGuildID)

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	})

	HandleMessage(session, textMessage("!probe"))

	if !*called {
		t.Error("a command with the configured prefix did not run when the server prefix was unreadable")
	}
}

func TestHandleMessageRefusesAdminCommandsForPlainMembers(t *testing.T) {
	session, requests := dispatchSession(t)
	called := registerProbeCommand(t, "probeadmin", true)
	registerProbeAlias(t, "probeadmin", "probeadmin")

	HandleMessage(session, textMessage("!probeadmin", dispatchMemberRole))

	if *called {
		t.Error("an admin-only handler ran for a member without the admin role")
	}
	sent := requests()
	if len(sent) != 1 || sent[0].Method != "POST" || sent[0].Path != "/channels/222/messages" {
		t.Errorf("sent %v, want the refusal posted to the channel", sent)
	}
}

func TestHandleMessageRunsAdminCommandsFromTheMessagesOwnRoles(t *testing.T) {
	session, requests := dispatchSession(t)
	called := registerProbeCommand(t, "probeadmin", true)
	registerProbeAlias(t, "probeadmin", "probeadmin")

	HandleMessage(session, textMessage("!probeadmin", dispatchAdminRole))

	if !*called {
		t.Error("an admin-only handler did not run for a member with the admin role")
	}
	for _, request := range requests() {
		if strings.Contains(request.Path, "/members/") {
			t.Errorf("requested %s %s, want no member lookup", request.Method, request.Path)
		}
	}
}

func TestHandleMessageReportsAFailingHandlerThatSentNothing(t *testing.T) {
	session, requests := dispatchSession(t)
	called := registerProbe(t, "probefail", false, failingHandler)
	registerProbeAlias(t, "probefail", "probefail")

	HandleMessage(session, textMessage("!probefail"))

	if !*called {
		t.Error("the failing handler did not run")
	}
	sent := requests()
	if len(sent) != 1 || sent[0].Method != "POST" || sent[0].Path != "/channels/222/messages" {
		t.Errorf("sent %v, want the error posted to the channel", sent)
	}
}

func TestHandleMessageKeepsQuietWhenAFailingHandlerAlreadyReplied(t *testing.T) {
	session, requests := dispatchSession(t)
	called := registerProbe(t, "probereply", false, func(s *discordgo.Session, i *discordgo.InteractionCreate) error {
		discord.RespondEmbed(s, i, &discordgo.MessageEmbed{Title: "partial"})
		return errors.New("probe failed after replying")
	})
	registerProbeAlias(t, "probereply", "probereply")

	HandleMessage(session, textMessage("!probereply"))

	if !*called {
		t.Error("the handler did not run")
	}
	if sent := requests(); len(sent) != 1 {
		t.Errorf("sent %d requests, want only the handler's own reply", len(sent))
	}
}
