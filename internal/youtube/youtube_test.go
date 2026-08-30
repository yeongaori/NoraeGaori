package youtube

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"noraegaori/internal/testutil"
	ytdlpUpdater "noraegaori/internal/ytdlp"
)

func useVersionManager(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())

	if err := ytdlpUpdater.InitVersionManager(); err != nil {
		t.Fatalf("InitVersionManager returned %v, want nil", err)
	}
	t.Cleanup(func() { ytdlpUpdater.InitVersionManager() })

	versionmanager := ytdlpUpdater.GetVersionManager()
	versionmanager.RegisterVersion("2026.08.19", filepath.Join("lib", "yt-dlp-2026.08.19", "yt-dlp"))
	versionmanager.SetActiveVersion("2026.08.19")
}

func readCanaryRing(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("data", "ytdlp_versions.json"))
	if err != nil {
		t.Fatalf("the version state was never persisted: %v", err)
	}

	var state struct {
		CanaryRing []string `json:"canary_ring"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("the persisted state is not valid JSON: %v", err)
	}
	return state.CanaryRing
}

func TestSaveVersionResultStoresBareVideoIDs(t *testing.T) {
	useVersionManager(t)

	saveVersionResult("https://music.youtube.com/watch?v=SGmfjVIGUcY", nil)

	ring := readCanaryRing(t)
	if len(ring) != 1 {
		t.Fatalf("got canary ring %v, want exactly one entry", ring)
	}
	if ring[0] != "SGmfjVIGUcY" {
		t.Errorf("got %q, want the bare video ID: a full URL here is pasted into the canary watch template", ring[0])
	}
}

func TestSaveVersionResultIgnoresURLsWithNoVideoID(t *testing.T) {
	useVersionManager(t)

	saveVersionResult("https://www.youtube.com/playlist?list=PLabc", nil)

	if ring := readCanaryRing(t); len(ring) != 0 {
		t.Errorf("got canary ring %v, want it left empty when no video ID can be read", ring)
	}
}

func TestGetVideoInfoRejectsLinksWithNoVideoID(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"channel handle", "https://www.youtube.com/@somechannel/videos"},
		{"legacy channel path", "https://www.youtube.com/channel/UCsomethinglong"},
		{"search results", "https://www.youtube.com/results?search_query=lofi"},
		{"pure playlist", "https://www.youtube.com/playlist?list=PL_x-9Ab_cd"},
		{"open redirect", "https://www.youtube.com/redirect?q=http://attacker.example/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var initialized int64
			testutil.Swap(t, &innertubeClient, nil)
			testutil.Swap(t, &innertubeOnce, &sync.Once{})
			testutil.Swap(t, &innertubeInit, func() {
				atomic.AddInt64(&initialized, 1)
				innertubeClient = &InnertubeClient{clientName: "TEST"}
			})

			song, err := GetVideoInfo("guild", tc.url, "requester", "requesterID")

			if !errors.Is(err, ErrUnsupportedYouTubeURL) {
				t.Errorf("err = %v, want ErrUnsupportedYouTubeURL", err)
			}
			if song != nil {
				t.Errorf("song = %+v, want nil", song)
			}
			if got := atomic.LoadInt64(&initialized); got != 0 {
				t.Errorf("the innertube client was built %d times, want 0 network calls", got)
			}
		})
	}
}
