package command_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord/command"
)

func guildOnlyCommand(name string) *discordgo.ApplicationCommand {
	contexts := []discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	return &discordgo.ApplicationCommand{
		Name:        name,
		Description: "probe",
		Contexts:    &contexts,
	}
}

func dmCapableCommand(name string) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        name,
		Description: "probe",
	}
}

func TestCanonicalContextsTreatsNilAsEveryContext(t *testing.T) {
	got := command.HookCanonicalContexts(nil)

	want := []discordgo.InteractionContextType{
		discordgo.InteractionContextGuild,
		discordgo.InteractionContextBotDM,
		discordgo.InteractionContextPrivateChannel,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestCanonicalContextsIsOrderIndependent(t *testing.T) {
	forward := []discordgo.InteractionContextType{
		discordgo.InteractionContextGuild,
		discordgo.InteractionContextBotDM,
	}
	reversed := []discordgo.InteractionContextType{
		discordgo.InteractionContextBotDM,
		discordgo.InteractionContextGuild,
	}

	first := command.HookCanonicalContexts(&forward)
	second := command.HookCanonicalContexts(&reversed)

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("got %v and %v, want the same canonical order", first, second)
		}
	}
}

func TestCanonicalContextsDoesNotMutateItsInput(t *testing.T) {
	contexts := []discordgo.InteractionContextType{
		discordgo.InteractionContextPrivateChannel,
		discordgo.InteractionContextGuild,
	}

	command.HookCanonicalContexts(&contexts)

	if contexts[0] != discordgo.InteractionContextPrivateChannel {
		t.Errorf("canonicalContexts reordered the caller's slice: %v", contexts)
	}
}

func TestDiffCommandSetsReportsGuildOnlyDrift(t *testing.T) {
	desired, err := command.HookCanonicalCommandMap([]*discordgo.ApplicationCommand{guildOnlyCommand("play")})
	if err != nil {
		t.Fatalf("failed to canonicalize the desired commands: %v", err)
	}
	existing, err := command.HookCanonicalCommandMap([]*discordgo.ApplicationCommand{dmCapableCommand("play")})
	if err != nil {
		t.Fatalf("failed to canonicalize the existing commands: %v", err)
	}

	added, updated, removed := command.HookDiffCommandSets(desired, existing)

	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("got added %v and removed %v, want neither", added, removed)
	}
	if len(updated) != 1 || updated[0] != "play" {
		t.Fatalf("got updated %v, want [play] so a deployed bot re-syncs its contexts", updated)
	}
}

func TestDiffCommandSetsIgnoresCommandsAlreadyGuildOnly(t *testing.T) {
	desired, err := command.HookCanonicalCommandMap([]*discordgo.ApplicationCommand{guildOnlyCommand("play")})
	if err != nil {
		t.Fatalf("failed to canonicalize the desired commands: %v", err)
	}
	existing, err := command.HookCanonicalCommandMap([]*discordgo.ApplicationCommand{guildOnlyCommand("play")})
	if err != nil {
		t.Fatalf("failed to canonicalize the existing commands: %v", err)
	}

	added, updated, removed := command.HookDiffCommandSets(desired, existing)

	if len(added) != 0 || len(updated) != 0 || len(removed) != 0 {
		t.Fatalf("got added %v, updated %v, removed %v, want no changes", added, updated, removed)
	}
}
