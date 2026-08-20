package automix

import (
	"os"
	"testing"

	"noraegaori/internal/messages"
)

func TestMain(m *testing.M) {
	if err := messages.LoadLocale("en"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
