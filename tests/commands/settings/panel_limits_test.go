package settings_test

import (
	"fmt"
	"testing"

	"noraegaori/internal/commands/settings"
	"noraegaori/internal/config"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/configtest"
)

func readOn(string) (string, error) {
	return settings.HookValueOn, nil
}

func TestThePickerStopsAtDiscordsOptionLimit(t *testing.T) {
	specs := make([]settings.HookSettingSpec, 0, 30)
	for index := range 30 {
		specs = append(specs, *settings.HookBuildSettingSpec(settings.HookSettingSpecFields{Key: fmt.Sprintf("toggle%d", index), Category: settings.HookCategoryPlayback, Kind: settings.HookSettingToggle, Read: readOn}))
	}
	testutil.Swap(t, settings.HookSettingSpecs, specs)

	menu := rowMenu(t, settings.HookSettingRow(settings.HookNewPanelView(checkGuildID, settings.HookCategoryPlayback, true)))

	if len(menu.Options) != settings.HookSelectOptionLimit {
		t.Errorf("the picker offers %d options, want %d", len(menu.Options), settings.HookSelectOptionLimit)
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
	spec := settings.HookBuildSettingSpec(settings.HookSettingSpecFields{Key: "manychoices", Kind: settings.HookSettingChoice, HasDefault: true, Options: func() []string { return values }})

	options := settings.HookChoiceOptions(checkGuildID, spec, "value3")

	if len(options) != settings.HookSelectOptionLimit {
		t.Errorf("the choice list offers %d options, want %d", len(options), settings.HookSelectOptionLimit)
	}
	if options[0].Value != settings.HookDefaultChoiceValue || options[0].Default {
		t.Errorf("the first option is %+v, want an unselected default", options[0])
	}
	if !options[4].Default || options[4].Value != "value3" {
		t.Errorf("option 4 is %+v, want the preselected current value", options[4])
	}
}

func TestTheDefaultCategoryFallsBackWhenNothingIsVisible(t *testing.T) {
	testutil.Swap(t, settings.HookSettingSpecs, []settings.HookSettingSpec{*settings.HookBuildSettingSpec(settings.HookSettingSpecFields{Key: "secret", Category: settings.HookCategoryGeneral, Kind: settings.HookSettingText, AdminOnly: true})})

	if got := settings.HookDefaultCategory(false); got != settings.HookCategoryPlayback {
		t.Errorf("defaultCategory = %q with nothing visible, want %q", got, settings.HookCategoryPlayback)
	}
}

func TestLabelsFallBackToTheirKey(t *testing.T) {
	if got := settings.HookSettingLabel(checkGuildID, "unlabelled"); got != "unlabelled" {
		t.Errorf("settingLabel = %q, want the key", got)
	}
	if got := settings.HookCategoryLabel(checkGuildID, "uncategorised"); got != "uncategorised" {
		t.Errorf("categoryLabel = %q, want the key", got)
	}
}

func TestNextValueOnlyFlipsToggles(t *testing.T) {
	if got := settings.HookNextValue(specFor(t, "prefix"), "abc"); got != "abc" {
		t.Errorf("nextValue = %q for a text setting, want it unchanged", got)
	}
	if got := settings.HookNextValue(specFor(t, "repeat"), settings.HookValueRepeatAll); got != settings.HookValueRepeatAll {
		t.Errorf("nextValue = %q for a choice setting, want it unchanged", got)
	}
	if got := settings.HookNextValue(specFor(t, "sponsorblock"), settings.HookValueOn); got != settings.HookValueOff {
		t.Errorf("nextValue = %q for a toggle that was on, want off", got)
	}
}

func TestDefaultForReadsTheConfiguredPrefixAndLanguage(t *testing.T) {
	if config.GetConfig() == nil {
		if got := settings.HookDefaultFor(specFor(t, "prefix")); got != "" {
			t.Errorf("defaultFor(prefix) = %q without a config, want empty", got)
		}
	}

	configtest.Setup(t)

	for key, want := range map[string]string{"prefix": "!", "language": "en", "volume": ""} {
		if got := settings.HookDefaultFor(specFor(t, key)); got != want {
			t.Errorf("defaultFor(%s) = %q, want %q", key, got, want)
		}
	}
}
