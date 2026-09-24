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

func WhileClosed(t *testing.T, run func()) {
	t.Helper()

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	run()
	if err := database.Initialize(); err != nil {
		t.Fatalf("failed to reopen the test database: %v", err)
	}
}

func CloseUntilCleanup(t *testing.T) {
	t.Helper()

	if err := database.Close(); err != nil {
		t.Fatalf("failed to close the test database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Initialize(); err != nil {
			t.Errorf("failed to reopen the test database: %v", err)
		}
	})
}
