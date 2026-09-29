package logtest

import (
	"os"
	"path/filepath"
	"testing"

	"noraegaori/internal/logger"
)

func Capture(t *testing.T) func() string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.log")
	logger.SetLogFile(path)
	t.Cleanup(func() { logger.SetLogFile("") })

	return func() string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read the captured log: %v", err)
		}
		return string(content)
	}
}
