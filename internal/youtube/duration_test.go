package youtube

import "testing"

func TestParseDurationToSeconds(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"Unknown", 0},
		{"🔴 LIVE", 0},
		{"0:30", 30},
		{"3:25", 205},
		{"1:00:00", 3600},
		{"1:01:01", 3661},
		{"10:00", 600},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := ParseDurationToSeconds(c.in); got != c.want {
			t.Errorf("ParseDurationToSeconds(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
