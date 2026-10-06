package locales_test

import (
	"encoding/json"
	"slices"
	"testing"

	"noraegaori/internal/commands/automix"
	"noraegaori/internal/messages"
	"noraegaori/locales"
)

func parseLocale(t *testing.T, file string) *messages.Locale {
	t.Helper()
	content, err := locales.Files.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read %s: %v", file, err)
	}
	var locale messages.Locale
	if err := json.Unmarshal(content, &locale); err != nil {
		t.Fatalf("failed to parse %s: %v", file, err)
	}
	return &locale
}

func TestEveryTransitionChoiceHasALabel(t *testing.T) {
	for _, file := range []string{"en.json", "ko.json"} {
		for _, key := range automix.MissingLabels(&parseLocale(t, file).AutoMixPanel) {
			t.Errorf("%s has no AutoMix panel label for %s", file, key)
		}
	}
}

func TestMissingLabelsAreFound(t *testing.T) {
	locale := parseLocale(t, "en.json")
	delete(locale.AutoMixPanel.StyleLabels, "fx.phaser")
	delete(locale.AutoMixPanel.CategoryLabels, "beatmatch")

	missing := automix.MissingLabels(&locale.AutoMixPanel)
	if !slices.Contains(missing, "fx.phaser") || !slices.Contains(missing, "beatmatch") || len(missing) != 2 {
		t.Errorf("missing labels = %v, want fx.phaser and beatmatch", missing)
	}
}
