package playback

import "testing"

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
