package discord

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateToLimit(t *testing.T) {
	if got := TruncateToLimit("hello", 10); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	got := TruncateToLimit("abcdefghij", 5)
	if len(got) > 5 {
		t.Errorf("result %q exceeds limit 5", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated result should end with ..., got %q", got)
	}

	multibyte := strings.Repeat("가", 20)
	tr := TruncateToLimit(multibyte, 10)
	if len(tr) > 10 {
		t.Errorf("multibyte result %q exceeds limit", tr)
	}
	if !utf8.ValidString(tr) {
		t.Errorf("truncation split a rune: %q", tr)
	}
}

func TestSplitLinesIntoChunks(t *testing.T) {
	lines := []string{"aaa", "bbb", "ccc", "ddd"}
	chunks := SplitLinesIntoChunks(lines, 8)
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
