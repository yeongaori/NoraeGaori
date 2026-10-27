package youtube_test

import (
	"noraegaori/internal/youtube"
)

import (
	"slices"
	"testing"
)

type urlAnalysisCase struct {
	name       string
	url        string
	wantType   youtube.URLType
	wantVideo  string
	wantPlayer string
}

func TestAnalyzeYouTubeURL(t *testing.T) {
	cases := slices.Concat(playlistURLCases(), videoURLCases())

	for i := range cases {
		tc := &cases[i]
		t.Run(tc.name, func(t *testing.T) {
			got := youtube.AnalyzeYouTubeURL(tc.url)

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

func playlistURLCases() []urlAnalysisCase {
	return []urlAnalysisCase{
		{
			name:       "pure playlist",
			url:        "https://www.youtube.com/playlist?list=PL_x-9Ab_cd",
			wantType:   youtube.URLTypePurePlaylist,
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:       "playlist without scheme",
			url:        "youtube.com/playlist?list=PL_x-9Ab_cd",
			wantType:   youtube.URLTypePurePlaylist,
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:       "watch inside playlist",
			url:        "https://www.youtube.com/watch?v=a_b-cD3fG_1&list=PL_x-9Ab_cd",
			wantType:   youtube.URLTypeVideoWithPlaylist,
			wantVideo:  "a_b-cD3fG_1",
			wantPlayer: "PL_x-9Ab_cd",
		},
		{
			name:       "short link inside playlist",
			url:        "https://youtu.be/a_b-cD3fG_1?list=PL_x-9Ab_cd",
			wantType:   youtube.URLTypeVideoWithPlaylist,
			wantVideo:  "a_b-cD3fG_1",
			wantPlayer: "PL_x-9Ab_cd",
		},
	}
}

func videoURLCases() []urlAnalysisCase {
	return []urlAnalysisCase{
		{
			name:      "plain watch link",
			url:       "https://www.youtube.com/watch?v=a_b-cD3fG_1",
			wantType:  youtube.URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:      "plain short link",
			url:       "https://youtu.be/a_b-cD3fG_1",
			wantType:  youtube.URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:      "music subdomain watch link",
			url:       "https://music.youtube.com/watch?v=a_b-cD3fG_1",
			wantType:  youtube.URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:      "mobile subdomain watch link",
			url:       "https://m.youtube.com/watch?v=a_b-cD3fG_1",
			wantType:  youtube.URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:      "shorts link",
			url:       "https://www.youtube.com/shorts/a_b-cD3fG_1",
			wantType:  youtube.URLTypeVideoOnly,
			wantVideo: "a_b-cD3fG_1",
		},
		{
			name:     "unrecognized url",
			url:      "https://example.com/video",
			wantType: youtube.URLTypeVideoOnly,
		},
	}
}
