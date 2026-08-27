package commands

import (
	"fmt"
	"noraegaori/internal/discord/command"
	"sort"
	"testing"

	"noraegaori/internal/messages"
)

var goldenCommands = []string{
	"automixpanel|admin=false|textonly=false|opts=1|handler=true|autocomplete=false",
	"automixstyle|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
	"automix|admin=false|textonly=false|opts=2|handler=true|autocomplete=false",
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

var goldenAliases = []string{
	"automix=automix",
	"automixpanel=automixpanel",
	"automixstyle=automixstyle",
	"cf=crossfade",
	"config=settings",
	"crossfade=crossfade",
	"dc=leave",
	"disconnect=leave",
	"fade-in=fadein",
	"fade-out=fadeout",
	"fadein=fadein",
	"fadeonstop=fadeonstop",
	"fadeout=fadeout",
	"forceremove=forceremove",
	"forceskip=forceskip",
	"forcestop=forcestop",
	"fos=fadeonstop",
	"fr=forceremove",
	"fs=forceskip",
	"fstop=forcestop",
	"h=help",
	"help=help",
	"j=join",
	"join=join",
	"jump=seek",
	"lang=setlanguage",
	"language=setlanguage",
	"leave=leave",
	"mix=automix",
	"mixpanel=automixpanel",
	"mixstyle=automixstyle",
	"move=switchvc",
	"movetrack=movetrack",
	"mt=movetrack",
	"normalization=normalization",
	"normalize=normalization",
	"nowplaying=nowplaying",
	"np=nowplaying",
	"p=play",
	"pause=pause",
	"play=play",
	"playnext=playnext",
	"pn=playnext",
	"prefix=setprefix",
	"q=queue",
	"queue=queue",
	"remove=remove",
	"repeat=repeat",
	"resume=resume",
	"rm=remove",
	"s=search",
	"sb=sponsorblock",
	"search=search",
	"seek=seek",
	"setlang=setlanguage",
	"setlanguage=setlanguage",
	"setprefix=setprefix",
	"settings=settings",
	"settingspanel=settings",
	"showstartedtrack=showstartedtrack",
	"showtrack=showstartedtrack",
	"skipto=skipto",
	"sponsorblock=sponsorblock",
	"st=skipto",
	"stop=stop",
	"switch=switchvc",
	"switchvc=switchvc",
	"trim=trimsilence",
	"trimsilence=trimsilence",
	"v=volume",
	"vol=volume",
	"volume=volume",
}

func registeredCommandFingerprint(t *testing.T) ([]string, []string) {
	t.Helper()

	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("failed to load the English locale: %v", err)
	}

	InitializeCommands()

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

func TestRegisteredCommandsMatchTheGoldenSet(t *testing.T) {
	fingerprints, _ := registeredCommandFingerprint(t)

	if len(fingerprints) != len(goldenCommands) {
		t.Fatalf("got %d registered commands, want %d", len(fingerprints), len(goldenCommands))
	}
	for i, got := range fingerprints {
		if got != goldenCommands[i] {
			t.Errorf("command %d: got %q, want %q", i, got, goldenCommands[i])
		}
	}
}

func TestRegisteredAliasesMatchTheGoldenSet(t *testing.T) {
	_, pairs := registeredCommandFingerprint(t)

	if len(pairs) != len(goldenAliases) {
		t.Fatalf("got %d aliases, want %d", len(pairs), len(goldenAliases))
	}
	for i, got := range pairs {
		if got != goldenAliases[i] {
			t.Errorf("alias %d: got %q, want %q", i, got, goldenAliases[i])
		}
	}
}

func TestEveryAliasTargetsARegisteredCommand(t *testing.T) {
	fingerprints, pairs := registeredCommandFingerprint(t)

	registered := make(map[string]bool, len(fingerprints))
	for _, fingerprint := range fingerprints {
		registered[fingerprint[:len(fingerprint)-len(fingerprint[indexOfPipe(fingerprint):])]] = true
	}

	for _, pair := range pairs {
		target := pair[indexOfEquals(pair)+1:]
		if !registered[target] {
			t.Errorf("alias %q points at %q, which is not registered", pair, target)
		}
	}
}

func indexOfPipe(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return i
		}
	}
	return len(s)
}

func indexOfEquals(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return len(s)
}
