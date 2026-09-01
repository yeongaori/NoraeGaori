package automix

import "testing"

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
