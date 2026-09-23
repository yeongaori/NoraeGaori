package settings

import (
	"fmt"
	"testing"

	"noraegaori/internal/config"
	"noraegaori/internal/testutil"
	"noraegaori/internal/testutil/configtest"
)

func readOn(string) (string, error) {
	return valueOn, nil
}

func TestThePickerStopsAtDiscordsOptionLimit(t *testing.T) {
	specs := make([]settingSpec, 0, 30)
	for index := range 30 {
		specs = append(specs, settingSpec{key: fmt.Sprintf("toggle%d", index), category: categoryPlayback, kind: settingToggle, read: readOn})
	}
	testutil.Swap(t, &settingSpecs, specs)

	menu := rowMenu(t, settingRow(newPanelView(checkGuildID, categoryPlayback, true)))

	if len(menu.Options) != selectOptionLimit {
		t.Errorf("the picker offers %d options, want %d", len(menu.Options), selectOptionLimit)
	}
	if menu.Options[0].Label != "toggle0" {
		t.Errorf("an unlabelled setting is shown as %q, want its key", menu.Options[0].Label)
	}
}

func TestTheChoiceListStopsAtDiscordsOptionLimit(t *testing.T) {
	values := make([]string, 0, 30)
	for index := range 30 {
		values = append(values, fmt.Sprintf("value%d", index))
	}
	spec := &settingSpec{key: "manychoices", kind: settingChoice, hasDefault: true, options: func() []string { return values }}

	options := choiceOptions(checkGuildID, spec, "value3")

	if len(options) != selectOptionLimit {
		t.Errorf("the choice list offers %d options, want %d", len(options), selectOptionLimit)
	}
	if options[0].Value != defaultChoiceValue || options[0].Default {
		t.Errorf("the first option is %+v, want an unselected default", options[0])
	}
	if !options[4].Default || options[4].Value != "value3" {
		t.Errorf("option 4 is %+v, want the preselected current value", options[4])
	}
}

func TestTheDefaultCategoryFallsBackWhenNothingIsVisible(t *testing.T) {
	testutil.Swap(t, &settingSpecs, []settingSpec{{key: "secret", category: categoryGeneral, kind: settingText, adminOnly: true}})

	if got := defaultCategory(false); got != categoryPlayback {
		t.Errorf("defaultCategory = %q with nothing visible, want %q", got, categoryPlayback)
	}
}

func TestLabelsFallBackToTheirKey(t *testing.T) {
	if got := settingLabel(checkGuildID, "unlabelled"); got != "unlabelled" {
		t.Errorf("settingLabel = %q, want the key", got)
	}
	if got := categoryLabel(checkGuildID, "uncategorised"); got != "uncategorised" {
		t.Errorf("categoryLabel = %q, want the key", got)
	}
}

func TestNextValueOnlyFlipsToggles(t *testing.T) {
	if got := nextValue(specFor(t, "prefix"), "abc"); got != "abc" {
		t.Errorf("nextValue = %q for a text setting, want it unchanged", got)
	}
	if got := nextValue(specFor(t, "repeat"), valueRepeatAll); got != valueRepeatAll {
		t.Errorf("nextValue = %q for a choice setting, want it unchanged", got)
	}
	if got := nextValue(specFor(t, "sponsorblock"), valueOn); got != valueOff {
		t.Errorf("nextValue = %q for a toggle that was on, want off", got)
	}
}

func TestDefaultForReadsTheConfiguredPrefixAndLanguage(t *testing.T) {
	if config.GetConfig() == nil {
		if got := defaultFor(specFor(t, "prefix")); got != "" {
			t.Errorf("defaultFor(prefix) = %q without a config, want empty", got)
		}
	}

	configtest.Setup(t)

	for key, want := range map[string]string{"prefix": "!", "language": "en", "volume": ""} {
		if got := defaultFor(specFor(t, key)); got != want {
			t.Errorf("defaultFor(%s) = %q, want %q", key, got, want)
		}
	}
}
