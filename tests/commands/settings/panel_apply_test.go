package settings_test

import (
	"errors"
	"testing"

	"noraegaori/internal/commands/settings"
	"noraegaori/tests/testutil/dbtest"
)

func specFor(t *testing.T, key string) *settings.HookSettingSpec {
	t.Helper()

	spec, found := settings.HookFindSetting(key)
	if !found {
		t.Fatalf("no setting spec registered for %q", key)
	}
	return spec
}

func TestTogglesAndRepeatRejectUnknownValues(t *testing.T) {
	for key, value := range map[string]string{"sponsorblock": "maybe", "repeat": "double"} {
		if _, err := settings.HookNormalizeValue(specFor(t, key), value); !errors.Is(err, *settings.HookErrUnknownValue) {
			t.Errorf("%s accepted %q, want errUnknownValue (got %v)", key, value, err)
		}
	}
}

func TestTogglesAndRepeatAcceptAnyCaseAndTheOnAlias(t *testing.T) {
	cases := []struct{ key, input, want string }{
		{"sponsorblock", "ON", settings.HookValueOn},
		{"sponsorblock", " off ", settings.HookValueOff},
		{"repeat", "Single", settings.HookValueRepeatSingle},
		{"repeat", "on", settings.HookValueRepeatAll},
	}
	for _, c := range cases {
		got, err := settings.HookNormalizeValue(specFor(t, c.key), c.input)
		if err != nil || got != c.want {
			t.Errorf("%s %q = (%q, %v), want %q", c.key, c.input, got, err, c.want)
		}
	}
}

func TestNumberSettingsRejectNonNumbers(t *testing.T) {
	for index := range *settings.HookSettingSpecs {
		spec := &(*settings.HookSettingSpecs)[index]
		if *spec.HookKind() != settings.HookSettingNumber {
			continue
		}
		for _, value := range []string{"", "abc", "1,5", "3 seconds"} {
			if _, err := settings.HookNormalizeValue(spec, value); !errors.Is(err, *settings.HookErrNotNumber) {
				t.Errorf("%s accepted %q, got err %v, want errNotNumber", *spec.HookKey(), value, err)
			}
		}
	}
}

func TestNumberSettingsRejectValuesOutsideTheirRange(t *testing.T) {
	for index := range *settings.HookSettingSpecs {
		spec := &(*settings.HookSettingSpecs)[index]
		if *spec.HookKind() != settings.HookSettingNumber {
			continue
		}
		for _, value := range []string{settings.HookFormatFloat(*spec.HookMin() - 1), settings.HookFormatFloat(*spec.HookMax() + 1)} {
			if _, err := settings.HookNormalizeValue(spec, value); !errors.Is(err, *settings.HookErrOutOfRange) {
				t.Errorf("%s accepted %q, got err %v, want errOutOfRange", *spec.HookKey(), value, err)
			}
		}
	}
}

func TestNumberSettingsAcceptTheirBoundaries(t *testing.T) {
	for index := range *settings.HookSettingSpecs {
		spec := &(*settings.HookSettingSpecs)[index]
		if *spec.HookKind() != settings.HookSettingNumber {
			continue
		}
		for _, value := range []float64{*spec.HookMin(), *spec.HookMax()} {
			normalized, err := settings.HookNormalizeValue(spec, settings.HookFormatFloat(value))
			if err != nil {
				t.Errorf("%s rejected boundary %g: %v", *spec.HookKey(), value, err)
				continue
			}
			if normalized != settings.HookFormatFloat(value) {
				t.Errorf("%s normalized %g to %q", *spec.HookKey(), value, normalized)
			}
		}
	}
}

func TestNumberSettingsTrimSurroundingSpace(t *testing.T) {
	spec := specFor(t, "volume")

	normalized, err := settings.HookNormalizeValue(spec, "  50  ")
	if err != nil {
		t.Fatalf("volume rejected a padded value: %v", err)
	}
	if normalized != "50" {
		t.Errorf("got %q, want \"50\"", normalized)
	}
}

func TestPrefixRejectsValuesLongerThanFiveRunes(t *testing.T) {
	spec := specFor(t, "prefix")

	if _, err := settings.HookNormalizeValue(spec, "abcdef"); !errors.Is(err, *settings.HookErrTooLong) {
		t.Errorf("got err %v, want errTooLong", err)
	}
	if _, err := settings.HookNormalizeValue(spec, "가나다라마"); err != nil {
		t.Errorf("a five rune prefix was rejected: %v", err)
	}
}

func TestPrefixAcceptsAnEmptyValueAsAReset(t *testing.T) {
	spec := specFor(t, "prefix")

	normalized, err := settings.HookNormalizeValue(spec, "   ")
	if err != nil {
		t.Fatalf("an empty prefix was rejected: %v", err)
	}
	if normalized != "" {
		t.Errorf("got %q, want an empty string", normalized)
	}
}

func TestTogglesFlipBetweenOnAndOff(t *testing.T) {
	spec := specFor(t, "sponsorblock")

	if next := settings.HookNextValue(spec, settings.HookValueOff); next != settings.HookValueOn {
		t.Errorf("got %q, want %q", next, settings.HookValueOn)
	}
	if next := settings.HookNextValue(spec, settings.HookValueOn); next != settings.HookValueOff {
		t.Errorf("got %q, want %q", next, settings.HookValueOff)
	}
}

func TestEverySettingIsLocalized(t *testing.T) {
	panel := settings.HookPanelStrings("")

	for index := range *settings.HookSettingSpecs {
		spec := &(*settings.HookSettingSpecs)[index]
		if panel.Labels[*spec.HookKey()] == "" {
			t.Errorf("setting %q has no label in settings_panel.labels", *spec.HookKey())
		}
		if *spec.HookKind() != settings.HookSettingText && *spec.HookKind() != settings.HookSettingNumber {
			continue
		}
		if panel.Hints[*spec.HookKey()] == "" {
			t.Errorf("editable setting %q has no hint in settings_panel.hints", *spec.HookKey())
		}
	}
}

func TestEveryCategoryIsLocalizedAndUsed(t *testing.T) {
	panel := settings.HookPanelStrings("")

	for _, category := range *settings.HookSettingCategories {
		if panel.Categories[category] == "" {
			t.Errorf("category %q has no label in settings_panel.categories", category)
		}
		if len(settings.HookSettingsInCategory(category, true)) == 0 {
			t.Errorf("category %q holds no settings", category)
		}
	}

	for index := range *settings.HookSettingSpecs {
		spec := &(*settings.HookSettingSpecs)[index]
		if !settings.HookIsKnownCategory(*spec.HookCategory()) {
			t.Errorf("setting %q sits in unknown category %q", *spec.HookKey(), *spec.HookCategory())
		}
	}
}

func TestCategoryChoicesCoverEveryCategory(t *testing.T) {
	choices := settings.BuildCategoryChoices()

	if len(choices) != len(*settings.HookSettingCategories) {
		t.Fatalf("got %d choices, want %d", len(choices), len(*settings.HookSettingCategories))
	}
	for index, choice := range choices {
		if choice.Value != (*settings.HookSettingCategories)[index] {
			t.Errorf("choice %d is %v, want %q", index, choice.Value, (*settings.HookSettingCategories)[index])
		}
	}
}

func TestThePanelStoresPrefixesInLowercase(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "prefix")
	if err := settings.HookApplySetting(checkGuildID, spec, "AB"); err != nil {
		t.Fatalf("failed to set the prefix: %v", err)
	}

	stored, ok := settings.HookCurrentValue(checkGuildID, spec)
	if !ok {
		t.Fatal("could not read the prefix back")
	}
	if stored != "ab" {
		t.Errorf("stored %q, want \"ab\"", stored)
	}
}
