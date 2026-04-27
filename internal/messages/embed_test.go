package messages

import "testing"

func TestEscapeLinkText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "oddloop", "oddloop"},
		{"pipes", `Music Video || Frederic "oddloop"`, `Music Video || Frederic "oddloop"`},
		{"asterisks", "*sweet*magic*", "*sweet*magic*"},
		{"underscore", "odd_loop", "odd_loop"},
		{"tilde", "~track~", "~track~"},
		{"backtick", "code`here`", "code`here`"},
		{"brackets", "[MV] foo", "［MV］ foo"},
		{"close-only", "weird]title", "weird］title"},
		{"backslash", `path\to\file`, `path\\to\\file`},
		{"mixed", `[MV] track || *bold*`, `［MV］ track || *bold*`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EscapeLinkText(c.in)
			if got != c.want {
				t.Errorf("EscapeLinkText(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestFormatBoldMaskedLink(t *testing.T) {
	got := FormatBoldMaskedLink(`[MV] foo`, `https://example.com/watch?v=abc`)
	want := `**[［MV］ foo](https://example.com/watch?v=abc)**`
	if got != want {
		t.Errorf("FormatBoldMaskedLink = %q, want %q", got, want)
	}
}

func TestFormatMaskedLinkPipes(t *testing.T) {
	got := FormatMaskedLink(`Music Video || Frederic "oddloop"`, `https://example.com/x`)
	want := `[Music Video || Frederic "oddloop"](https://example.com/x)`
	if got != want {
		t.Errorf("FormatMaskedLink = %q, want %q", got, want)
	}
}

func TestEscapeURLClosingParen(t *testing.T) {
	got := EscapeURL(`https://example.com/watch?v=abc(def)`)
	want := `https://example.com/watch?v=abc(def%29`
	if got != want {
		t.Errorf("EscapeURL = %q, want %q", got, want)
	}
}
