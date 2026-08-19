package commands

import (
	"fmt"
	"sync"
	"testing"

	"noraegaori/internal/messages"
)

func seedTestCommands(t *testing.T, count int) {
	t.Helper()

	commandsMu.Lock()
	previousCommands := commands
	previousAliases := aliases
	commands = make(map[string]*Command, count)
	aliases = make(map[string]string, count)
	commandsMu.Unlock()

	t.Cleanup(func() {
		commandsMu.Lock()
		commands = previousCommands
		aliases = previousAliases
		commandsMu.Unlock()
	})

	for i := 0; i < count; i++ {
		name := fmt.Sprintf("cmd%d", i)
		RegisterCommand(&Command{Name: name, Description: "original"})
		RegisterAlias(fmt.Sprintf("a%d", i), name)
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
				lookupCommand(name)
				lookupAlias(fmt.Sprintf("a%d", worker))
				snapshotCommands()
			}
		}(i)
	}

	for i := 0; i < 50; i++ {
		ReloadAliases()
	}

	close(stop)
	wg.Wait()
}

func TestReloadAliasesDoesNotMutateHeldCommands(t *testing.T) {
	seedTestCommands(t, 1)

	held, ok := lookupCommand("cmd0")
	if !ok {
		t.Fatal("the seeded command was not registered")
	}
	if held.Description != "original" {
		t.Fatalf("got description %q, want %q", held.Description, "original")
	}

	ReloadAliases()

	if held.Description != "original" {
		t.Errorf("a command held by a handler was mutated in place to %q", held.Description)
	}
}

func TestRegisterCommandIsVisibleThroughLookup(t *testing.T) {
	seedTestCommands(t, 0)

	RegisterCommand(&Command{Name: "solo", Description: "d"})
	RegisterAlias("s", "solo")

	if _, ok := lookupCommand("solo"); !ok {
		t.Error("the registered command is not visible through lookupCommand")
	}
	if target, ok := lookupAlias("s"); !ok || target != "solo" {
		t.Errorf("lookupAlias returned (%q, %v), want (\"solo\", true)", target, ok)
	}
	if snapshot := snapshotCommands(); len(snapshot) != 1 {
		t.Errorf("got %d commands in the snapshot, want 1", len(snapshot))
	}
}
