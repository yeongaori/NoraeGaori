package youtube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
