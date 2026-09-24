package command

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/tests/testutil/discordtest"
)

const syncAppID = "sync-app"

func syncCommands() []*Command {
	return []*Command{
		{Name: "alpha", Description: "Alpha", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "text", Description: "Text"}}},
		{Name: "beta", Description: "Beta"},
		{Name: "gamma", Description: "Gamma", TextOnly: true},
	}
}

func registeredJSON(t *testing.T) string {
	t.Helper()

	guildContexts := []discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	registered := make([]*discordgo.ApplicationCommand, 0, 2)
	for _, cmd := range syncCommands() {
		if cmd.TextOnly {
			continue
		}
		registered = append(registered, &discordgo.ApplicationCommand{Name: cmd.Name, Description: cmd.Description, Options: cmd.Options, Contexts: &guildContexts})
	}

	raw, err := json.Marshal(registered)
	if err != nil {
		t.Fatalf("failed to encode the registered commands: %v", err)
	}
	return string(raw)
}

func syncSession(t *testing.T, existing string, fetchStatus, overwriteStatus int) (*discordgo.Session, func() []discordtest.Request) {
	t.Helper()

	useRegistry(t, syncCommands()...)
	session, requests := discordtest.StubAPIResponder(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return fetchStatus, existing
		}
		return overwriteStatus, "[]"
	})
	session.State.User = &discordgo.User{ID: syncAppID}
	return session, requests
}

func TestSlashCommandsSkipTheOverwriteWhenAlreadyInSync(t *testing.T) {
	session, requests := syncSession(t, registeredJSON(t), http.StatusOK, http.StatusOK)

	if err := RegisterSlashCommands(session); err != nil {
		t.Fatalf("RegisterSlashCommands returned %v", err)
	}
	if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodGet {
		t.Errorf("sent %v, want only the lookup of the registered commands", sent)
	}
}

func TestSlashCommandsOverwriteChangedCommandsWithoutTextOnlyOnes(t *testing.T) {
	session, requests := syncSession(t, "[]", http.StatusOK, http.StatusOK)

	if err := RegisterSlashCommands(session); err != nil {
		t.Fatalf("RegisterSlashCommands returned %v", err)
	}

	sent := requests()
	if len(sent) != 2 || sent[1].Method != http.MethodPut || !strings.HasSuffix(sent[1].Path, "/applications/"+syncAppID+"/commands") {
		t.Fatalf("sent %v, want the lookup followed by an overwrite", sent)
	}

	var overwritten []discordgo.ApplicationCommand
	if err := json.Unmarshal(sent[1].RawBody, &overwritten); err != nil {
		t.Fatalf("failed to decode the overwrite: %v", err)
	}
	names := make([]string, 0, len(overwritten))
	for index := range overwritten {
		names = append(names, overwritten[index].Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"alpha", "beta"}) {
		t.Errorf("overwrote %v, want alpha and beta without the text-only gamma", names)
	}
}

func TestSlashCommandSyncReportsDiscordFailures(t *testing.T) {
	for name, check := range map[string]struct {
		fetchStatus     int
		overwriteStatus int
		want            string
	}{
		"a failed lookup":    {http.StatusForbidden, http.StatusOK, "failed to get existing commands"},
		"a failed overwrite": {http.StatusOK, http.StatusForbidden, "failed to bulk overwrite"},
	} {
		t.Run(name, func(t *testing.T) {
			session, _ := syncSession(t, "[]", check.fetchStatus, check.overwriteStatus)

			if err := RegisterSlashCommands(session); err == nil || !strings.Contains(err.Error(), check.want) {
				t.Errorf("RegisterSlashCommands returned %v, want an error containing %q", err, check.want)
			}
		})
	}
}

func TestMissingDescriptionsFallBackToNames(t *testing.T) {
	commands := []*discordgo.ApplicationCommand{
		{Name: "blank", Options: []*discordgo.ApplicationCommandOption{{Name: "empty"}, {Name: "filled", Description: "Filled"}}},
		{Name: "described", Description: "Described"},
	}

	fillMissingCommandDescriptions(commands)

	if commands[0].Description != "blank" || commands[0].Options[0].Description != "empty" {
		t.Errorf("missing descriptions became %q and %q, want the names", commands[0].Description, commands[0].Options[0].Description)
	}
	if commands[0].Options[1].Description != "Filled" || commands[1].Description != "Described" {
		t.Error("an existing description was replaced")
	}
}
