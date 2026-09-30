package play_test

import (
	"container/list"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/play"
)

func resetAutocompleteState() {
	play.HookAutocompleteCacheMutex.Lock()
	*play.HookAutocompleteCacheEntries = make(map[string]*play.HookAutocompleteCacheEntry)
	*play.HookAutocompleteCacheOrder = list.New()
	play.HookAutocompleteCacheMutex.Unlock()

	play.HookAutocompleteGatesMutex.Lock()
	*play.HookAutocompleteGates = make(map[string]*play.HookAutocompleteGateState)
	play.HookAutocompleteGatesMutex.Unlock()
}

func testChoices(name string) []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{{Name: name, Value: name}}
}

func TestBuildVideoChoiceName(t *testing.T) {
	longTitle := strings.Repeat("a", 200)
	longKoreanTitle := strings.Repeat("가", 200)

	testCases := []struct {
		name     string
		title    string
		channel  string
		duration string
		check    func(t *testing.T, got string)
	}{
		{
			name:     "Short title is untouched",
			title:    "Never Gonna Give You Up",
			channel:  "Rick Astley",
			duration: "3:33",
			check: func(t *testing.T, got string) {
				if got != "Never Gonna Give You Up - Rick Astley (3:33)" {
					t.Errorf("got %q", got)
				}
			},
		},
		{
			name:     "Long title keeps the channel and duration tail",
			title:    longTitle,
			channel:  "Rick Astley",
			duration: "3:33",
			check: func(t *testing.T, got string) {
				if count := utf8.RuneCountInString(got); count != play.HookMaxChoiceNameRunes {
					t.Errorf("rune count = %d, want %d", count, play.HookMaxChoiceNameRunes)
				}
				if !strings.HasSuffix(got, " - Rick Astley (3:33)") {
					t.Errorf("tail was truncated: %q", got)
				}
			},
		},
		{
			name:     "Long Korean title truncates on a rune boundary",
			title:    longKoreanTitle,
			channel:  "이지금",
			duration: "4:56",
			check: func(t *testing.T, got string) {
				if !utf8.ValidString(got) {
					t.Errorf("produced invalid UTF-8: %q", got)
				}
				if count := utf8.RuneCountInString(got); count != play.HookMaxChoiceNameRunes {
					t.Errorf("rune count = %d, want %d", count, play.HookMaxChoiceNameRunes)
				}
				if !strings.HasSuffix(got, " - 이지금 (4:56)") {
					t.Errorf("tail was truncated: %q", got)
				}
			},
		},
		{
			name:     "Empty duration omits the parenthetical",
			title:    "Ditto",
			channel:  "HYBE LABELS",
			duration: "",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "(") {
					t.Errorf("expected no parenthetical, got %q", got)
				}
				if got != "Ditto - HYBE LABELS" {
					t.Errorf("got %q", got)
				}
			},
		},
		{
			name:     "Empty channel omits the dash segment",
			title:    "Ditto",
			channel:  "",
			duration: "3:06",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, " - ") {
					t.Errorf("expected no dash segment, got %q", got)
				}
				if got != "Ditto (3:06)" {
					t.Errorf("got %q", got)
				}
			},
		},
		{
			name:     "Overlong channel is clamped without starving the title",
			title:    longTitle,
			channel:  strings.Repeat("c", 120),
			duration: "3:33",
			check: func(t *testing.T, got string) {
				if count := utf8.RuneCountInString(got); count > play.HookMaxChoiceNameRunes {
					t.Errorf("rune count = %d, want <= %d", count, play.HookMaxChoiceNameRunes)
				}
				if strings.HasPrefix(got, " - ") {
					t.Errorf("title was starved: %q", got)
				}
				if !strings.HasPrefix(got, "aaa") {
					t.Errorf("expected the title to lead, got %q", got)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.check(t, play.HookBuildVideoChoiceName(testCase.title, testCase.channel, testCase.duration))
		})
	}
}

func TestNormalizeAutocompleteQuery(t *testing.T) {
	testCases := []struct {
		name  string
		query string
		want  string
	}{
		{"Lowercases", "Never Gonna", "never gonna"},
		{"Collapses whitespace runs", "never    gonna", "never gonna"},
		{"Trailing space matches the bare query", "never ", "never"},
		{"Leading space matches the bare query", " never", "never"},
		{"Whitespace only becomes empty", "   ", ""},
		{"Korean is preserved", "뉴진스 노래", "뉴진스 노래"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := play.HookNormalizeAutocompleteQuery(testCase.query); got != testCase.want {
				t.Errorf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestAutocompleteCacheEviction(t *testing.T) {
	resetAutocompleteState()

	for index := 0; index < play.HookAutocompleteCacheCapacity; index++ {
		key := "suggest|en|q" + strconv.Itoa(index)
		play.HookSaveAutocompleteChoices(key, testChoices(key))
	}

	if (*play.HookAutocompleteCacheOrder).Len() != play.HookAutocompleteCacheCapacity {
		t.Fatalf("list length = %d, want %d", (*play.HookAutocompleteCacheOrder).Len(), play.HookAutocompleteCacheCapacity)
	}

	if _, found := play.HookLoadAutocompleteChoices("suggest|en|q0"); !found {
		t.Fatal("q0 should still be cached")
	}

	play.HookSaveAutocompleteChoices("suggest|en|overflow", testChoices("overflow"))

	if (*play.HookAutocompleteCacheOrder).Len() != play.HookAutocompleteCacheCapacity {
		t.Errorf("list length = %d, want %d", (*play.HookAutocompleteCacheOrder).Len(), play.HookAutocompleteCacheCapacity)
	}
	if len(*play.HookAutocompleteCacheEntries) != play.HookAutocompleteCacheCapacity {
		t.Errorf("map length = %d, want %d", len(*play.HookAutocompleteCacheEntries), play.HookAutocompleteCacheCapacity)
	}
	if _, found := play.HookLoadAutocompleteChoices("suggest|en|q0"); !found {
		t.Error("q0 was recently used and should have survived eviction")
	}
	if _, found := play.HookLoadAutocompleteChoices("suggest|en|q1"); found {
		t.Error("q1 was least recently used and should have been evicted")
	}
}

func TestAutocompleteCacheExpiry(t *testing.T) {
	resetAutocompleteState()

	key := "suggest|en|never"
	play.HookSaveAutocompleteChoices(key, testChoices("never gonna give you up"))

	play.HookAutocompleteCacheMutex.Lock()
	*(*play.HookAutocompleteCacheEntries)[key].HookTimestamp() = time.Now().Add(-play.HookAutocompleteCacheTTL - time.Second)
	play.HookAutocompleteCacheMutex.Unlock()

	if _, found := play.HookLoadAutocompleteChoices(key); found {
		t.Fatal("expired entry should report a miss")
	}
	if len(*play.HookAutocompleteCacheEntries) != 0 {
		t.Errorf("map length = %d, want 0", len(*play.HookAutocompleteCacheEntries))
	}
	if (*play.HookAutocompleteCacheOrder).Len() != 0 {
		t.Errorf("list length = %d, want 0", (*play.HookAutocompleteCacheOrder).Len())
	}
}

func TestLoadAutocompleteChoicesReturnsCopy(t *testing.T) {
	resetAutocompleteState()

	key := "video|en|ditto"
	play.HookSaveAutocompleteChoices(key, testChoices("original"))

	first, found := play.HookLoadAutocompleteChoices(key)
	if !found {
		t.Fatal("entry should be cached")
	}
	first[0] = &discordgo.ApplicationCommandOptionChoice{Name: "mutated", Value: "mutated"}

	second, found := play.HookLoadAutocompleteChoices(key)
	if !found {
		t.Fatal("entry should still be cached")
	}
	if second[0].Name != "original" {
		t.Errorf("cache was corrupted by the caller, got %q", second[0].Name)
	}
}

func TestLoadNearestAutocompleteChoices(t *testing.T) {
	resetAutocompleteState()

	play.HookSaveAutocompleteChoices(play.HookAutocompleteCacheKey("suggest", "en", "ne"), testChoices("ne"))
	play.HookSaveAutocompleteChoices(play.HookAutocompleteCacheKey("suggest", "en", "never"), testChoices("never"))

	testCases := []struct {
		name  string
		kind  string
		query string
		want  string
	}{
		{"Exact match wins", "suggest", "never", "never"},
		{"Longest cached prefix wins", "suggest", "never gonna give", "never"},
		{"Shorter prefix is used when nothing longer is cached", "suggest", "nex", "ne"},
		{"Different kind does not match", "video", "never gonna give", ""},
		{"Nothing cached returns nil", "suggest", "zz", ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := play.HookLoadNearestAutocompleteChoices(testCase.kind, "en", testCase.query)
			if testCase.want == "" {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Name != testCase.want {
				t.Errorf("got %v, want %q", got, testCase.want)
			}
		})
	}
}

func TestAutocompleteDebounceSupersedes(t *testing.T) {
	resetAutocompleteState()

	first := play.HookBeginAutocompleteFetch("user1", "guild1", "play")
	second := play.HookBeginAutocompleteFetch("user1", "guild1", "play")

	if first == second {
		t.Fatalf("generations should differ, both were %d", first)
	}
	if play.HookIsLatestAutocompleteFetch("user1", "guild1", "play", first) {
		t.Error("the superseded generation should not be latest")
	}
	if !play.HookIsLatestAutocompleteFetch("user1", "guild1", "play", second) {
		t.Error("the newest generation should be latest")
	}
}

func TestAutocompleteDebounceIsolation(t *testing.T) {
	resetAutocompleteState()

	guildOne := play.HookBeginAutocompleteFetch("user1", "guild1", "play")
	play.HookBeginAutocompleteFetch("user1", "guild2", "play")
	play.HookBeginAutocompleteFetch("user1", "guild1", "search")
	play.HookBeginAutocompleteFetch("user2", "guild1", "play")

	if !play.HookIsLatestAutocompleteFetch("user1", "guild1", "play", guildOne) {
		t.Error("another guild, command, or user should not supersede this fetch")
	}
	if play.HookIsLatestAutocompleteFetch("user3", "guild9", "play", 1) {
		t.Error("an unknown key should never report as latest")
	}
}
