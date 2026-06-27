package commands

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClampFadeDuration(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0, 1},
		{0.5, 1},
		{1, 1},
		{15, 15},
		{30, 30},
		{40, 30},
	}
	for _, c := range cases {
		if got := clampFadeDuration(c.in); got != c.want {
			t.Errorf("clampFadeDuration(%g) = %g, want %g", c.in, got, c.want)
		}
	}
}

func TestClampAutoMixBeats(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0, 4},
		{3, 4},
		{4, 4},
		{32, 32},
		{64, 64},
		{100, 64},
	}
	for _, c := range cases {
		if got := clampAutoMixBeats(c.in); got != c.want {
			t.Errorf("clampAutoMixBeats(%g) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestBoolToEmoji(t *testing.T) {
	if boolToEmoji(true) != "✅" {
		t.Error("true should be ✅")
	}
	if boolToEmoji(false) != "❌" {
		t.Error("false should be ❌")
	}
}

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0:00"},
		{5, "0:05"},
		{65, "1:05"},
		{600, "10:00"},
		{3661, "1:01:01"},
		{3600, "1:00:00"},
	}
	for _, c := range cases {
		if got := formatSeconds(c.in); got != c.want {
			t.Errorf("formatSeconds(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSeekPosition(t *testing.T) {
	valid := []struct {
		in   string
		want int
	}{
		{"90", 90000},
		{"1:30", 90000},
		{"1:00:00", 3600000},
		{"0:00", 0},
		{"1:30.5", 90500},
		{"61", 61000},
	}
	for _, c := range valid {
		got, err := parseSeekPosition(c.in)
		if err != nil {
			t.Errorf("parseSeekPosition(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSeekPosition(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	invalid := []string{"", "abc", "1:2:3:4", "1:60", "-5", "1:-5"}
	for _, in := range invalid {
		if _, err := parseSeekPosition(in); err == nil {
			t.Errorf("parseSeekPosition(%q) should error", in)
		}
	}
}

func TestTruncateToLimit(t *testing.T) {
	if got := truncateToLimit("hello", 10); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	got := truncateToLimit("abcdefghij", 5)
	if len(got) > 5 {
		t.Errorf("result %q exceeds limit 5", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated result should end with ..., got %q", got)
	}

	multibyte := strings.Repeat("가", 20)
	tr := truncateToLimit(multibyte, 10)
	if len(tr) > 10 {
		t.Errorf("multibyte result %q exceeds limit", tr)
	}
	if !utf8.ValidString(tr) {
		t.Errorf("truncation split a rune: %q", tr)
	}
}

func TestSplitLinesIntoChunks(t *testing.T) {
	lines := []string{"aaa", "bbb", "ccc", "ddd"}
	chunks := splitLinesIntoChunks(lines, 8)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	for _, c := range chunks {
		if len(c) > 8 {
			t.Errorf("chunk %q exceeds limit 8", c)
		}
	}

	joined := strings.ReplaceAll(strings.Join(chunks, "\n"), "\n", "")
	if joined != "aaabbbcccddd" {
		t.Errorf("chunks lost/reordered content: %q", joined)
	}
}

func TestCreateProgressBar(t *testing.T) {
	live := createProgressBar(0, "🔴 LIVE")
	if utf8.RuneCountInString(live) != 10 {
		t.Errorf("live bar should be 10 runes, got %d", utf8.RuneCountInString(live))
	}
	if strings.Contains(live, "🔘") {
		t.Error("unparseable duration should have no knob")
	}

	start := createProgressBar(0, "3:00")
	if utf8.RuneCountInString(start) != 10 {
		t.Errorf("bar should be 10 runes, got %d", utf8.RuneCountInString(start))
	}
	if !strings.Contains(start, "🔘") {
		t.Error("in-progress bar should have a knob")
	}

	past := createProgressBar(999000, "3:00")
	if strings.Contains(past, "🔘") {
		t.Error("completed bar should have no knob (progress capped)")
	}
}
