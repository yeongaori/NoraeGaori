package play

import (
	"fmt"
	"os"
	"testing"

	"noraegaori/internal/messages"
)

func TestMain(m *testing.M) {
	if err := messages.LoadLocale("en"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load the English locale for tests: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
