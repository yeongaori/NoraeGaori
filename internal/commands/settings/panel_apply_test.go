package settings

import (
	"errors"
	"testing"

	"noraegaori/internal/testutil/dbtest"
)

func specFor(t *testing.T, key string) settingSpec {
	t.Helper()

	spec, found := findSetting(key)
	if !found {
		t.Fatalf("no setting spec registered for %q", key)
	}
	return spec
}

func TestNumberSettingsRejectNonNumbers(t *testing.T) {
	for _, spec := range settingSpecs {
		if spec.kind != settingNumber {
			continue
		}
		for _, value := range []string{"", "abc", "1,5", "3 seconds"} {
			if _, err := normalizeValue(spec, value); !errors.Is(err, errNotNumber) {
				t.Errorf("%s accepted %q, got err %v, want errNotNumber", spec.key, value, err)
			}
		}
	}
}

func TestNumberSettingsRejectValuesOutsideTheirRange(t *testing.T) {
	for _, spec := range settingSpecs {
		if spec.kind != settingNumber {
			continue
		}
		for _, value := range []string{formatFloat(spec.min - 1), formatFloat(spec.max + 1)} {
			if _, err := normalizeValue(spec, value); !errors.Is(err, errOutOfRange) {
				t.Errorf("%s accepted %q, got err %v, want errOutOfRange", spec.key, value, err)
			}
		}
	}
}

func TestNumberSettingsAcceptTheirBoundaries(t *testing.T) {
	for _, spec := range settingSpecs {
		if spec.kind != settingNumber {
			continue
		}
		for _, value := range []float64{spec.min, spec.max} {
			normalized, err := normalizeValue(spec, formatFloat(value))
			if err != nil {
				t.Errorf("%s rejected boundary %g: %v", spec.key, value, err)
				continue
			}
			if normalized != formatFloat(value) {
				t.Errorf("%s normalized %g to %q", spec.key, value, normalized)
			}
		}
	}
}

func TestNumberSettingsTrimSurroundingSpace(t *testing.T) {
	spec := specFor(t, "volume")

	normalized, err := normalizeValue(spec, "  50  ")
	if err != nil {
		t.Fatalf("volume rejected a padded value: %v", err)
	}
	if normalized != "50" {
		t.Errorf("got %q, want \"50\"", normalized)
	}
}

func TestPrefixRejectsValuesLongerThanFiveRunes(t *testing.T) {
	spec := specFor(t, "prefix")

	if _, err := normalizeValue(spec, "abcdef"); !errors.Is(err, errTooLong) {
		t.Errorf("got err %v, want errTooLong", err)
	}
	if _, err := normalizeValue(spec, "가나다라마"); err != nil {
		t.Errorf("a five rune prefix was rejected: %v", err)
	}
}

func TestPrefixAcceptsAnEmptyValueAsAReset(t *testing.T) {
	spec := specFor(t, "prefix")

	normalized, err := normalizeValue(spec, "   ")
	if err != nil {
		t.Fatalf("an empty prefix was rejected: %v", err)
	}
	if normalized != "" {
		t.Errorf("got %q, want an empty string", normalized)
	}
}

func TestTogglesFlipBetweenOnAndOff(t *testing.T) {
	spec := specFor(t, "sponsorblock")

	if next := nextValue(spec, valueOff); next != valueOn {
		t.Errorf("got %q, want %q", next, valueOn)
	}
	if next := nextValue(spec, valueOn); next != valueOff {
		t.Errorf("got %q, want %q", next, valueOff)
	}
}

func TestRepeatCyclesThroughEveryMode(t *testing.T) {
	spec := specFor(t, "repeat")

	value := valueRepeatOff
	seen := []string{value}
	for range repeatValues {
		value = nextValue(spec, value)
		seen = append(seen, value)
	}

	want := []string{valueRepeatOff, valueRepeatAll, valueRepeatSingle, valueRepeatOff}
	for index, expected := range want {
		if seen[index] != expected {
			t.Errorf("step %d is %q, want %q", index, seen[index], expected)
		}
	}
}

func TestRepeatCycleRecoversFromAnUnknownMode(t *testing.T) {
	spec := specFor(t, "repeat")

	if next := nextValue(spec, "nonsense"); next != valueRepeatOff {
		t.Errorf("got %q, want %q", next, valueRepeatOff)
	}
}

func TestSettingKeysSurviveACustomIDRoundTrip(t *testing.T) {
	token := "abc123"

	for _, spec := range settingSpecs {
		id := customID(modalPrefix, spec.key, token)
		if parsed := settingKeyFrom(id, modalPrefix, token); parsed != spec.key {
			t.Errorf("%q round-tripped to %q", spec.key, parsed)
		}
	}
}

func TestEverySettingIsLocalized(t *testing.T) {
	panel := panelStrings("")

	for _, spec := range settingSpecs {
		if panel.Labels[spec.key] == "" {
			t.Errorf("setting %q has no label in settings_panel.labels", spec.key)
		}
		if spec.kind != settingText && spec.kind != settingNumber {
			continue
		}
		if panel.Hints[spec.key] == "" {
			t.Errorf("editable setting %q has no hint in settings_panel.hints", spec.key)
		}
	}
}

func TestEveryCategoryIsLocalizedAndUsed(t *testing.T) {
	panel := panelStrings("")

	for _, category := range settingCategories {
		if panel.Categories[category] == "" {
			t.Errorf("category %q has no label in settings_panel.categories", category)
		}
		if len(settingsInCategory(category, true)) == 0 {
			t.Errorf("category %q holds no settings", category)
		}
	}

	for _, spec := range settingSpecs {
		if !isKnownCategory(spec.category) {
			t.Errorf("setting %q sits in unknown category %q", spec.key, spec.category)
		}
	}
}

func TestCategoryChoicesCoverEveryCategory(t *testing.T) {
	choices := BuildCategoryChoices()

	if len(choices) != len(settingCategories) {
		t.Fatalf("got %d choices, want %d", len(choices), len(settingCategories))
	}
	for index, choice := range choices {
		if choice.Value != settingCategories[index] {
			t.Errorf("choice %d is %v, want %q", index, choice.Value, settingCategories[index])
		}
	}
}

func TestThePanelAndSetPrefixAgreeOnLength(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "prefix")
	korean := "가나다라마"

	if len([]rune(korean)) > maxPrefixLength {
		t.Fatalf("the sample prefix %q is already over the limit", korean)
	}
	if _, err := normalizeValue(spec, korean); err != nil {
		t.Errorf("the panel rejected a %d rune prefix: %v", len([]rune(korean)), err)
	}
	if len([]rune(korean+"바")) <= maxPrefixLength {
		t.Fatal("the over-limit sample is not actually over the limit")
	}
	if _, err := normalizeValue(spec, korean+"바"); !errors.Is(err, errTooLong) {
		t.Errorf("the panel accepted an over-limit prefix, got err %v", err)
	}
}

func TestThePanelStoresPrefixesInLowercase(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "prefix")
	if err := applySetting(checkGuildID, spec, "AB"); err != nil {
		t.Fatalf("failed to set the prefix: %v", err)
	}

	stored, ok := currentValue(checkGuildID, spec)
	if !ok {
		t.Fatal("could not read the prefix back")
	}
	if stored != "ab" {
		t.Errorf("stored %q, want \"ab\"", stored)
	}
}
