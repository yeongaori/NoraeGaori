package youtube

import "testing"

func TestAnalyzeYouTubeURL(t *testing.T) {
	cases := []struct {
		name       string
		url        string
		wantType   URLType
		wantVideo  string
		wantPlayer string
	}{
		{
			name:       "pure playlist",
			url:        "https://www.youtube.com/playlist?list=PL_x-9Ab_cd",
			wantType:   URLTypePurePlaylist,
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:       "playlist without scheme",
			url:        "youtube.com/playlist?list=PL_x-9Ab_cd",
			wantType:   URLTypePurePlaylist,
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:       "watch inside playlist",
			url:        "https://www.youtube.com/watch?v=a_b-cD3fG_1&list=PL_x-9Ab_cd",
			wantType:   URLTypeVideoWithPlaylist,
			wantVideo:  "a_b-cD3fG_1",
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:       "short link inside playlist",
			url:        "https://youtu.be/a_b-cD3fG_1?list=PL_x-9Ab_cd",
			wantType:   URLTypeVideoWithPlaylist,
			wantVideo:  "a_b-cD3fG_1",
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:      "plain watch link",
			url:       "https://www.youtube.com/watch?v=a_b-cD3fG_1",
			wantType:  URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:      "plain short link",
			url:       "https://youtu.be/a_b-cD3fG_1",
			wantType:  URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:      "music subdomain watch link",
			url:       "https://music.youtube.com/watch?v=a_b-cD3fG_1",
			wantType:  URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:     "unrecognized url",
			url:      "https://example.com/video",
			wantType: URLTypeVideoOnly,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AnalyzeYouTubeURL(tc.url)

			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
			if got.VideoID != tc.wantVideo {
				t.Errorf("VideoID = %q, want %q", got.VideoID, tc.wantVideo)
			}
			if got.PlaylistID != tc.wantPlayer {
				t.Errorf("PlaylistID = %q, want %q", got.PlaylistID, tc.wantPlayer)
			}
		})
	}
}
