package configtest

import (
	"os"
	"path/filepath"
	"testing"

	"noraegaori/internal/config"
)

const testConfig = `{"prefix":"!","language":"en","default_volume":55}`

func Setup(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
	if err := os.MkdirAll("config", 0755); err != nil {
		t.Fatalf("failed to create the config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join("config", "config.json"), []byte(testConfig), 0644); err != nil {
		t.Fatalf("failed to write the test config: %v", err)
	}
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to load the test config: %v", err)
	}
	t.Cleanup(func() {
		if err := config.Close(); err != nil {
			t.Errorf("failed to close the test config: %v", err)
		}
	})
}
