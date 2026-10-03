package locales_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"noraegaori/locales"
)

func TestFilesEmbedEveryShippedLocale(t *testing.T) {
	onDisk, err := filepath.Glob(filepath.Join("..", "..", "locales", "*.json"))
	if err != nil || len(onDisk) < 2 {
		t.Fatalf("found shipped locales %v (err %v), want at least en.json and ko.json", onDisk, err)
	}
	embedded, err := fs.Glob(locales.Files, "*.json")
	if err != nil || len(embedded) != len(onDisk) {
		t.Fatalf("embedded locales = %v (err %v), want the %d files in locales/", embedded, err, len(onDisk))
	}

	for _, path := range onDisk {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		got, err := locales.Files.ReadFile(filepath.Base(path))
		if err != nil || string(got) != string(want) {
			t.Errorf("embedded %s differs from the file on disk (err %v)", filepath.Base(path), err)
		}
	}
}

func TestEnglishLocaleIsEmbedded(t *testing.T) {
	if len(locales.EnglishLocale) == 0 {
		t.Fatal("EnglishLocale is empty, so the //go:embed directive above it was lost")
	}
}

func TestEnglishLocaleParses(t *testing.T) {
	var locale map[string]any
	if err := json.Unmarshal(locales.EnglishLocale, &locale); err != nil {
		t.Fatalf("the embedded locale is not valid JSON: %v", err)
	}

	rpc, ok := locale["rpc"].(map[string]any)
	if !ok {
		t.Fatal("the embedded locale has no rpc section")
	}

	activity, ok := rpc["activity_default_1"].(string)
	if !ok || activity == "" {
		t.Errorf("rpc.activity_default_1 is %v, want a non-empty string", rpc["activity_default_1"])
	}
}
