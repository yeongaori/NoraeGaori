package commands_test

import (
	"fmt"
	"noraegaori/internal/commands"
	"noraegaori/internal/discord/command"
	"sort"
	"strings"
	"testing"

	"noraegaori/internal/messages"
)

var expectedCommands = []string{
	"autoleave|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"automixpanel|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"automixstyle|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"automix|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"autopause|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"autoresume|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"crossfade|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"fadein|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"fadeonstop|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"fadeout|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"forceremove|admin=true|textonly=true|opts=1|handler=true|autocomplete=false",
	"forceskip|admin=true|textonly=true|opts=0|handler=true|autocomplete=false",
	"forcestop|admin=true|textonly=true|opts=0|handler=true|autocomplete=false",
	"help|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"join|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"language|admin=true|textonly=true|opts=1|handler=true|autocomplete=false",
	"lang|admin=true|textonly=true|opts=1|handler=true|autocomplete=false",
	"leave|admin=false|textonly=false|opts=0|handler=true|autocomplete=false",
	"movetrack|admin=true|textonly=true|opts=2|handler=true|autocomplete=false",
	"normalization|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"nowplaying|admin=false|textonly=false|opts=0|handler=true|autocomplete=false",
	"pause|admin=false|textonly=false|opts=0|handler=true|autocomplete=false",
	"playnext|admin=false|textonly=false|opts=1|handler=true|autocomplete=true",
	"play|admin=false|textonly=false|opts=1|handler=true|autocomplete=true",
	"queue|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"remove|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"repeat|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"resume|admin=false|textonly=false|opts=0|handler=true|autocomplete=false",
	"search|admin=false|textonly=false|opts=1|handler=true|autocomplete=true",
	"seek|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"setlanguage|admin=true|textonly=true|opts=1|handler=true|autocomplete=false",
	"setprefix|admin=true|textonly=true|opts=1|handler=true|autocomplete=false",
	"settings|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"showstartedtrack|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"skipto|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"skip|admin=false|textonly=false|opts=0|handler=true|autocomplete=false",
	"sponsorblock|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"status|admin=true|textonly=true|opts=0|handler=true|autocomplete=false",
	"stop|admin=false|textonly=false|opts=0|handler=true|autocomplete=false",
	"swap|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"switchvc|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"trimsilence|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"volume|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
}

func fingerprintCommands(t *testing.T) ([]string, []string) {
	t.Helper()

	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("failed to load the English locale: %v", err)
	}

	commands.InitializeCommands()

	snapshot := command.Snapshot()
	fingerprints := make([]string, 0, len(snapshot))
	for name, cmd := range snapshot {
		fingerprints = append(fingerprints, fmt.Sprintf("%s|admin=%v|textonly=%v|opts=%d|handler=%v|autocomplete=%v",
			name, cmd.AdminOnly, cmd.TextOnly, len(cmd.Options), cmd.Handler != nil, cmd.AutocompleteHandler != nil))
	}
	sort.Strings(fingerprints)

	aliases := command.SnapshotAliases()
	pairs := make([]string, 0, len(aliases))
	for alias, target := range aliases {
		pairs = append(pairs, alias+"="+target)
	}
	sort.Strings(pairs)

	return fingerprints, pairs
}

func TestRegisteredCommandsList(t *testing.T) {
	fingerprints, _ := fingerprintCommands(t)

	if len(fingerprints) != len(expectedCommands) {
		t.Fatalf("got %d registered commands, want %d", len(fingerprints), len(expectedCommands))
	}
	for i, got := range fingerprints {
		if got != expectedCommands[i] {
			t.Errorf("command %d: got %q, want %q", i, got, expectedCommands[i])
		}
	}
}

func TestReloadKeepsStartupAliases(t *testing.T) {
	previousCommands := command.HookCommands.Load()
	previousAliases := command.HookAliases.Load()
	t.Cleanup(func() {
		command.HookCommands.Store(previousCommands)
		command.HookAliases.Store(previousAliases)
		if err := messages.LoadLocale("en"); err != nil {
			t.Errorf("failed to restore the English locale: %v", err)
		}
	})

	for _, lang := range messages.AvailableLocales() {
		if err := messages.LoadLocale(lang); err != nil {
			t.Fatalf("failed to load the %s locale: %v", lang, err)
		}
		command.HookCommands.Store(&map[string]*command.Command{})
		command.HookAliases.Store(&map[string]string{})

		commands.InitializeCommands()
		startup := command.SnapshotAliases()
		command.ReloadAliases()
		reloaded := command.SnapshotAliases()

		for alias, want := range reloaded {
			if got := startup[alias]; got != want {
				t.Errorf("locale %s, alias %q: got %q at startup, want %q after reload", lang, alias, got, want)
			}
		}
		for alias, got := range startup {
			if _, ok := reloaded[alias]; !ok {
				t.Errorf("locale %s, alias %q: got %q at startup, want %q after reload", lang, alias, got, "")
			}
		}
	}
}

func TestAliasesTargetCommands(t *testing.T) {
	fingerprints, pairs := fingerprintCommands(t)

	registered := make(map[string]bool, len(fingerprints))
	for _, fingerprint := range fingerprints {
		name, _, _ := strings.Cut(fingerprint, "|")
		registered[name] = true
	}

	for _, pair := range pairs {
		_, target, _ := strings.Cut(pair, "=")
		if !registered[target] {
			t.Errorf("alias %q points at %q, which is not registered", pair, target)
		}
	}
}

func TestAliasesAreUnique(t *testing.T) {
	t.Cleanup(func() {
		if err := messages.LoadLocale("en"); err != nil {
			t.Errorf("failed to restore the English locale: %v", err)
		}
	})

	for _, lang := range messages.AvailableLocales() {
		if err := messages.LoadLocale(lang); err != nil {
			t.Fatalf("failed to load the %s locale: %v", lang, err)
		}

		owners := make(map[string][]string)
		for name, cs := range messages.T().Commands {
			for _, alias := range cs.Aliases {
				owners[alias] = append(owners[alias], name)
			}
		}
		for alias, names := range owners {
			if len(names) > 1 {
				sort.Strings(names)
				t.Errorf("locale %s, alias %q: got commands %v, want one", lang, alias, names)
			}
		}
	}
}
