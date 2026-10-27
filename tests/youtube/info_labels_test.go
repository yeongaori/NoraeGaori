package youtube_test

import (
	"testing"

	"github.com/lrstanley/go-ytdlp"
	"noraegaori/internal/youtube"
)

func TestDurationLabelLiveUnknown(t *testing.T) {
	live := ytdlp.ExtractedLiveStatusIsLive
	upcoming := ytdlp.ExtractedLiveStatusIsUpcoming
	wasLive := ytdlp.ExtractedLiveStatusWasLive
	seconds := 61.0
	zero := 0.0

	cases := []struct {
		name       string
		info       *ytdlp.ExtractedInfo
		wantLabel  string
		wantIsLive bool
	}{
		{"a live stream", &ytdlp.ExtractedInfo{LiveStatus: &live, Duration: &seconds}, youtube.LiveDuration, true},
		{"an upcoming stream", &ytdlp.ExtractedInfo{LiveStatus: &upcoming}, youtube.LiveDuration, true},
		{"a finished stream", &ytdlp.ExtractedInfo{LiveStatus: &wasLive, Duration: &seconds}, "1:01", false},
		{"a video without a duration", &ytdlp.ExtractedInfo{Duration: &zero}, "Unknown", false},
		{"a video with no details", &ytdlp.ExtractedInfo{}, "Unknown", false},
	}

	for i := range cases {
		tc := &cases[i]
		label, isLive := youtube.HookDurationLabel(tc.info)
		if label != tc.wantLabel || isLive != tc.wantIsLive {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, label, isLive, tc.wantLabel, tc.wantIsLive)
		}
	}
}

func TestLastThumbnailLargest(t *testing.T) {
	cases := []struct {
		name string
		info *ytdlp.ExtractedInfo
		want string
	}{
		{"several thumbnails", &ytdlp.ExtractedInfo{Thumbnails: []*ytdlp.ExtractedThumbnail{
			{URL: "https://i.ytimg.com/small.jpg"},
			{URL: "https://i.ytimg.com/medium.jpg"},
			{URL: "https://i.ytimg.com/large.jpg"},
		}}, "https://i.ytimg.com/large.jpg"},
		{"no thumbnails", &ytdlp.ExtractedInfo{}, ""},
	}

	for i := range cases {
		tc := &cases[i]
		if got := youtube.HookLastThumbnail(tc.info); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
