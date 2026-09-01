package discord

import (
	"strings"
	"unicode/utf8"
)

func TruncateToLimit(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	cut := limit - 3
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func SplitLinesIntoChunks(lines []string, limit int) []string {
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		line = TruncateToLimit(line, limit)
		if current.Len() > 0 && current.Len()+1+len(line) > limit {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func TruncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
