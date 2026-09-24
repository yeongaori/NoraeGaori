package queue

import (
	"testing"

	"noraegaori/tests/testutil/dbtest"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	dbtest.Setup(t)
	if err := CreateQueue("guild1", "text_channel_test", "voice_channel_test"); err != nil {
		t.Fatalf("Failed to create test queue: %v", err)
	}
}
