package youtube

import "testing"

func TestParseYouTubeURLAcceptsEveryShapeUsersSend(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		wantVideo    string
		wantPlaylist string
	}{
		{name: "watch", url: "https://www.youtube.com/watch?v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "watch without scheme", url: "youtube.com/watch?v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "watch over http", url: "http://youtube.com/watch?v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "watch with uppercase host", url: "https://WWW.YouTube.COM/watch?v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "watch behind another parameter", url: "https://www.youtube.com/watch?app=desktop&v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "mobile watch", url: "https://m.youtube.com/watch?v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "music watch", url: "https://music.youtube.com/watch?v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "short link", url: "https://youtu.be/a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "short link with tracking", url: "https://youtu.be/a_b-cD3fG_1?si=FbAFmfYCH0ccc1EW", wantVideo: "a_b-cD3fG_1"},
		{name: "shorts", url: "https://www.youtube.com/shorts/a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "live", url: "https://www.youtube.com/live/a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "embed", url: "https://www.youtube.com/embed/a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "legacy embed", url: "https://www.youtube.com/v/a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1"},
		{name: "watch inside playlist", url: "https://www.youtube.com/watch?v=a_b-cD3fG_1&list=PL_x-9Ab_cd", wantVideo: "a_b-cD3fG_1", wantPlaylist: "PL_x-9Ab_cd"},
		{name: "playlist before video", url: "https://www.youtube.com/watch?list=PL_x-9Ab_cd&v=a_b-cD3fG_1", wantVideo: "a_b-cD3fG_1", wantPlaylist: "PL_x-9Ab_cd"},
		{name: "pure playlist", url: "https://www.youtube.com/playlist?list=PL_x-9Ab_cd", wantPlaylist: "PL_x-9Ab_cd"},
		{name: "channel handle", url: "https://www.youtube.com/@somechannel/videos"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, ok := parseYouTubeURL(tc.url)
			if !ok {
				t.Fatalf("parseYouTubeURL(%q) rejected a YouTube host", tc.url)
			}
			if parsed.VideoID != tc.wantVideo {
				t.Errorf("VideoID = %q, want %q", parsed.VideoID, tc.wantVideo)
			}
			if parsed.PlaylistID != tc.wantPlaylist {
				t.Errorf("PlaylistID = %q, want %q", parsed.PlaylistID, tc.wantPlaylist)
			}
		})
	}
}

func TestParseYouTubeURLRejectsHostsTheBotMustNeverFetch(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"userinfo before the real host", "https://youtube.com@attacker.com/a"},
		{"youtube as a subdomain", "https://youtube.com.attacker.com/a"},
		{"youtube inside the path", "https://attacker.com/youtube.com/a"},
		{"newline smuggling a second url", "https://youtube.com/x\nhttps://attacker.com/"},
		{"explicit port", "https://youtube.com:8443/x"},
		{"scheme relative", "//attacker.com/x"},
		{"trailing dot host", "https://youtube.com./x"},
		{"plain search term", "never gonna give you up"},
		{"unrelated media url", "https://example.com/song.mp3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parseYouTubeURL(tc.url); ok {
				t.Errorf("parseYouTubeURL(%q) accepted a host outside the allowlist", tc.url)
			}
			if IsYouTubeURL(tc.url) {
				t.Errorf("IsYouTubeURL(%q) = true, want false", tc.url)
			}
		})
	}
}
