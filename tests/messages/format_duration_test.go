package messages_test

import (
	"testing"

	"noraegaori/internal/messages"
)

func TestFormatDurationShowsHoursOnlyWhenNeeded(t *testing.T) {
	cases := []struct {
		seconds int
		want    string
	}{
		{0, "0:00"},
		{59, "0:59"},
		{61, "1:01"},
		{3599, "59:59"},
		{3600, "1:00:00"},
		{3661, "1:01:01"},
		{36000, "10:00:00"},
	}

	for _, tc := range cases {
		if got := messages.FormatDuration(tc.seconds); got != tc.want {
			t.Errorf("FormatDuration(%d): got %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
