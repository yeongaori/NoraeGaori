package command_test

import (
	"fmt"
	"sync"
	"testing"

	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"
)

func seedTestCommands(t *testing.T, count int) {
	t.Helper()

	previousCommands := command.HookCommands.Load()
	previousAliases := command.HookAliases.Load()
	command.HookCommands.Store(&map[string]*command.Command{})
	command.HookAliases.Store(&map[string]string{})

	t.Cleanup(func() {
		command.HookCommands.Store(previousCommands)
		command.HookAliases.Store(previousAliases)
	})

	for i := 0; i < count; i++ {
		name := fmt.Sprintf("cmd%d", i)
		command.RegisterCommand(&command.Command{Name: name, Description: "original"})
		command.RegisterAlias(fmt.Sprintf("a%d", i), name)
	}
}

func TestReloadAliasesRacesWithLookups(t *testing.T) {
	seedTestCommands(t, 40)

	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("failed to load the English locale: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				name := fmt.Sprintf("cmd%d", worker)
				command.HookLookupCommand(name)
				command.HookLookupAlias(fmt.Sprintf("a%d", worker))
				command.Snapshot()
			}
		}(i)
	}

	for i := 0; i < 50; i++ {
		command.ReloadAliases()
	}

	close(stop)
	wg.Wait()
}

func TestReloadAliasesAppliesTheLocaleWithoutMutatingHeldCommands(t *testing.T) {
	seedTestCommands(t, 0)
	command.RegisterCommand(&command.Command{Name: "play", Description: "original"})

	held, ok := command.HookLookupCommand("play")
	if !ok {
		t.Fatal("the seeded command was not registered")
	}

	command.ReloadAliases()

	if held.Description != "original" {
		t.Errorf("a command held by a handler was mutated in place to %q", held.Description)
	}
	localized := messages.T().Commands["play"]
	if localized.Description == "" || len(localized.Aliases) == 0 {
		t.Fatal("the English locale has no play description or aliases to apply")
	}
	reloaded, _ := command.HookLookupCommand("play")
	if reloaded.Description != localized.Description {
		t.Errorf("the reloaded description is %q, want the locale's %q", reloaded.Description, localized.Description)
	}
	for _, alias := range localized.Aliases {
		if name, found := command.HookLookupAlias(alias); !found || name != "play" {
			t.Errorf("the locale alias %q does not reach play", alias)
		}
	}
}

func TestRegisterCommandIsVisibleThroughLookup(t *testing.T) {
	seedTestCommands(t, 0)

	command.RegisterCommand(&command.Command{Name: "solo", Description: "d"})
	command.RegisterAlias("s", "solo")

	if _, ok := command.HookLookupCommand("solo"); !ok {
		t.Error("the registered command is not visible through lookupCommand")
	}
	if target, ok := command.HookLookupAlias("s"); !ok || target != "solo" {
		t.Errorf("lookupAlias returned (%q, %v), want (\"solo\", true)", target, ok)
	}
	if snapshot := command.Snapshot(); len(snapshot) != 1 {
		t.Errorf("got %d commands in the snapshot, want 1", len(snapshot))
	}
}
