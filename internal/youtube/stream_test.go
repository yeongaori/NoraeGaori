package youtube

import (
	"strings"
	"testing"
)

func argValue(args []string, flag string) (string, bool) {
	for idx, arg := range args {
		if arg == flag && idx+1 < len(args) {
			return args[idx+1], true
		}
	}
	return "", false
}

func hasArg(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func TestStreamPipeArgsAlwaysEndsWithTheURL(t *testing.T) {
	args := streamPipeArgs("https://example.invalid/watch?v=abc", false, 128000, 0)

	if len(args) == 0 || args[len(args)-1] != "https://example.invalid/watch?v=abc" {
		t.Errorf("got args %v, want the URL last so yt-dlp treats it as the target", args)
	}
	if !hasArg(args, "--no-playlist") {
		t.Error("--no-playlist is missing, so a URL with a list parameter would pull the whole playlist")
	}
	if output, ok := argValue(args, "--output"); !ok || output != "-" {
		t.Errorf("got --output %q, want %q so the audio reaches the pipe", output, "-")
	}
}

func TestStreamPipeArgsOmitsOptionalFlagsByDefault(t *testing.T) {
	args := streamPipeArgs("https://example.invalid/watch?v=abc", false, 128000, 0)

	for _, flag := range []string{"--sponsorblock-mark", "--sponsorblock-remove", "--download-sections"} {
		if hasArg(args, flag) {
			t.Errorf("%s was passed even though it was not requested", flag)
		}
	}
}

func TestStreamPipeArgsAddsSponsorBlockWhenRequested(t *testing.T) {
	args := streamPipeArgs("https://example.invalid/watch?v=abc", true, 128000, 0)

	if mark, ok := argValue(args, "--sponsorblock-mark"); !ok || mark != "all" {
		t.Errorf("got --sponsorblock-mark %q, want %q", mark, "all")
	}
	remove, ok := argValue(args, "--sponsorblock-remove")
	if !ok {
		t.Fatal("--sponsorblock-remove is missing")
	}
	for _, category := range []string{"sponsor", "selfpromo", "interaction", "intro", "outro"} {
		if !strings.Contains(remove, category) {
			t.Errorf("--sponsorblock-remove %q does not cover %q", remove, category)
		}
	}
}

func TestStreamPipeArgsConvertsSeekMillisecondsToASection(t *testing.T) {
	args := streamPipeArgs("https://example.invalid/watch?v=abc", false, 128000, 90500)

	section, ok := argValue(args, "--download-sections")
	if !ok {
		t.Fatal("--download-sections is missing for a seek request")
	}
	if section != "*90.5-inf" {
		t.Errorf("got %q, want %q for a 90500ms seek", section, "*90.5-inf")
	}
}

func TestStreamPipeArgsTracksTheRequestedBitrate(t *testing.T) {
	low := streamPipeArgs("https://example.invalid/watch?v=abc", false, 64000, 0)
	high := streamPipeArgs("https://example.invalid/watch?v=abc", false, 384000, 0)

	lowFormat, lowOK := argValue(low, "--format")
	highFormat, highOK := argValue(high, "--format")
	if !lowOK || !highOK {
		t.Fatal("--format is missing")
	}
	if lowFormat == "" || highFormat == "" {
		t.Error("an empty format would let yt-dlp pick an arbitrary stream")
	}
	if lowFormat == highFormat {
		t.Errorf("both bitrates produced format %q, so the channel bitrate is not influencing selection", lowFormat)
	}
}
