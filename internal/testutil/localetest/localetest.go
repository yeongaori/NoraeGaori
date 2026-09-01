package localetest

import (
	"fmt"
	"os"
	"testing"

	"noraegaori/internal/messages"
)

func Run(m *testing.M) {
	if err := messages.LoadLocale("en"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to load the English locale for tests: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
