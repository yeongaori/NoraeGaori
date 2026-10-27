package play_test

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/play"
	"noraegaori/internal/youtube"
)

func TestSearchResultsPanelNumbering(t *testing.T) {
	results := []youtube.SearchResult{{Title: "First", Duration: "1:00"}, {Title: "Second", Duration: "2:00"}}
	embed, components, _ := play.HookSearchResultsPanel("guild", "query", results, "msg123")

	options := components[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu).Options
	for i := range results {
		if want := fmt.Sprintf("msg123:%d", i); options[i].Value != want {
			t.Errorf("option %d value = %q, want %q", i, options[i].Value, want)
		}
		if want := fmt.Sprintf("%d. %s", i+1, results[i].Title); embed.Fields[i].Name != want {
			t.Errorf("field %d name = %q, want %q", i, embed.Fields[i].Name, want)
		}
	}
}

func TestParseSearchSelectionValid(t *testing.T) {
	cases := map[string]struct {
		searchID string
		index    int
	}{
		"msg123:0":  {"msg123", 0},
		"msg123:4":  {"msg123", 4},
		"abc:11":    {"abc", 11},
		"msg123:-1": {"msg123", -1},
	}

	for value, want := range cases {
		searchID, index, err := play.HookParseSearchSelection(value)
		if err != nil {
			t.Errorf("parseSearchSelection(%q) returned %v, want nil", value, err)
			continue
		}
		if searchID != want.searchID || index != want.index {
			t.Errorf("parseSearchSelection(%q) = (%q, %d), want (%q, %d)", value, searchID, index, want.searchID, want.index)
		}
	}
}

func TestParseSearchSelectionInvalid(t *testing.T) {
	cases := []string{
		"msg123",
		"msg123:",
		"msg123:abc",
		"msg123:1:2",
		"msg123: 1",
		"msg123:1.5",
		"",
	}

	for _, value := range cases {
		searchID, index, err := play.HookParseSearchSelection(value)
		if err == nil {
			t.Errorf("parseSearchSelection(%q) returned (%q, %d, nil), want a rejection", value, searchID, index)
		}
		if index != 0 || searchID != "" {
			t.Errorf("parseSearchSelection(%q) returned (%q, %d) alongside its error, want zero values", value, searchID, index)
		}
	}
}

func TestParseSearchSelectionNoFirst(t *testing.T) {
	_, index, err := play.HookParseSearchSelection("msg123:notanumber")
	if err == nil {
		t.Fatal("a non-numeric index was accepted, so the caller would queue result 0")
	}
	if index != 0 {
		t.Errorf("got index %d alongside the error, want 0", index)
	}
}
