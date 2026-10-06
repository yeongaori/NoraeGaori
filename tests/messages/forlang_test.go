package messages_test

import (
	"testing"

	"noraegaori/internal/messages"
)

func TestForLangLoadsTheRequestedLocale(t *testing.T) {
	english := messages.ForLang("en")
	korean := messages.ForLang("ko")
	if english == nil || korean == nil {
		t.Fatal("a shipped locale did not load")
	}
	if english.AutoMixPanel.SettingsTab == korean.AutoMixPanel.SettingsTab {
		t.Errorf("en and ko both read %q, want each locale's own text", english.AutoMixPanel.SettingsTab)
	}
	if again := messages.ForLang("ko"); again != korean {
		t.Error("a second lookup built a new locale, want the cached one")
	}
}

func TestForLangFallsBackToTheActiveLocale(t *testing.T) {
	if got := messages.ForLang("zz-missing"); got != messages.T() {
		t.Error("an unknown language did not fall back to the active locale")
	}
}
