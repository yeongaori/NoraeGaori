package dbtest

import (
	"testing"

	"noraegaori/internal/database"
)

func Setup(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
	if err := database.Initialize(); err != nil {
		t.Fatalf("failed to initialize the test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("failed to close the test database: %v", err)
		}
	})
}
