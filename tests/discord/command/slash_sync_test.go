package command_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/logger"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/logtest"
)

const syncAppID = "sync-app"

func syncCommands() []*command.Command {
	return []*command.Command{
		{Name: "test1", Description: "Test1", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "text", Description: "Text"}}},
		{Name: "test2", Description: "Test2"},
		{Name: "test3", Description: "Test3", TextOnly: true},
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

func TestSyncSlashCommandsInSync(t *testing.T) {
	session, requests := syncSession(t, registeredJSON(t), http.StatusOK, http.StatusOK)

	if err := command.SyncSlashCommands(session); err != nil {
		t.Fatalf("SyncSlashCommands returned %v", err)
	}
	if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodGet {
		t.Errorf("sent %v, want only the lookup of the registered commands", sent)
	}
}

func TestSyncSlashCommandsOverwrites(t *testing.T) {
	session, requests := syncSession(t, "[]", http.StatusOK, http.StatusOK)

	if err := command.SyncSlashCommands(session); err != nil {
		t.Fatalf("SyncSlashCommands returned %v", err)
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
	if !slices.Equal(names, []string{"test1", "test2"}) {
		t.Errorf("overwrote %v, want test1 and test2 without the text-only test3", names)
	}
}

func TestSyncSlashCommandsSkipsEmpty(t *testing.T) {
	for name, check := range map[string]struct {
		edit func([]*discordgo.ApplicationCommand) []*discordgo.ApplicationCommand
		want string
	}{
		"only added": {func(existing []*discordgo.ApplicationCommand) []*discordgo.ApplicationCommand {
			return existing[:1]
		}, "added: [test2]"},
		"only updated": {func(existing []*discordgo.ApplicationCommand) []*discordgo.ApplicationCommand {
			existing[1].Description = "Changed"
			return existing
		}, "updated: [test2]"},
		"only removed": {func(existing []*discordgo.ApplicationCommand) []*discordgo.ApplicationCommand {
			return append(existing, &discordgo.ApplicationCommand{Name: "test4", Description: "Test4"})
		}, "removed: [test4]"},
		"added and removed": {func(existing []*discordgo.ApplicationCommand) []*discordgo.ApplicationCommand {
			return append(existing[:1], &discordgo.ApplicationCommand{Name: "test4", Description: "Test4"})
		}, "added: [test2], removed: [test4]"},
	} {
		t.Run(name, func(t *testing.T) {
			var existing []*discordgo.ApplicationCommand
			if err := json.Unmarshal([]byte(registeredJSON(t)), &existing); err != nil {
				t.Fatalf("failed to decode the registered commands: %v", err)
			}
			raw, err := json.Marshal(check.edit(existing))
			if err != nil {
				t.Fatalf("failed to encode the existing commands: %v", err)
			}

			testutil.Swap(t, logger.HookDebugMode, true)
			read := logtest.CaptureConsole(t)
			session, _ := syncSession(t, string(raw), http.StatusOK, http.StatusOK)

			if err := command.SyncSlashCommands(session); err != nil {
				t.Fatalf("SyncSlashCommands returned %v", err)
			}
			if logged := read(); !strings.Contains(logged, "Slash command changes detected — "+check.want+"\n") {
				t.Errorf("got %q, want a line ending in %q", logged, check.want)
			}
		})
	}
}

func TestSyncSlashCommandsFailure(t *testing.T) {
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

			if err := command.SyncSlashCommands(session); err == nil || !strings.Contains(err.Error(), check.want) {
				t.Errorf("SyncSlashCommands returned %v, want an error containing %q", err, check.want)
			}
		})
	}
}

func TestFillDescriptionsUsesNames(t *testing.T) {
	commands := []*discordgo.ApplicationCommand{
		{Name: "blank", Options: []*discordgo.ApplicationCommandOption{{Name: "empty"}, {Name: "filled", Description: "Filled"}}},
		{Name: "described", Description: "Described"},
	}

	command.HookFillDescriptions(commands)

	if commands[0].Description != "blank" || commands[0].Options[0].Description != "empty" {
		t.Errorf("missing descriptions became %q and %q, want the names", commands[0].Description, commands[0].Options[0].Description)
	}
	if commands[0].Options[1].Description != "Filled" || commands[1].Description != "Described" {
		t.Error("an existing description was replaced")
	}
}
