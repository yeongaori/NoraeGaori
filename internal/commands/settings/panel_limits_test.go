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
	for index := 0; index < 30; index++ {
		specs = append(specs, settingSpec{key: fmt.Sprintf("toggle%d", index), category: categoryPlayback, kind: settingToggle, read: readOn})
	}
	testutil.Swap(t, &settingSpecs, specs)

	menu := rowMenu(t, settingRow(newPanelView(checkGuildID, categoryPlayback, "", checkToken, true)))

	if len(menu.Options) != selectOptionLimit {
		t.Errorf("the picker offers %d options, want %d", len(menu.Options), selectOptionLimit)
	}
	if menu.Options[0].Label != "toggle0" {
		t.Errorf("an unlabelled setting is shown as %q, want its key", menu.Options[0].Label)
	}
}

func TestTheValueListStopsAtDiscordsOptionLimit(t *testing.T) {
	values := make([]string, 0, 30)
	for index := 0; index < 30; index++ {
		values = append(values, fmt.Sprintf("value%d", index))
	}
	testutil.Swap(t, &settingSpecs, []settingSpec{{
		key:      "manychoices",
		category: categoryGeneral,
		kind:     settingChoice,
		options:  func() []string { return values },
		read:     func(string) (string, error) { return "", nil },
	}})

	components := buildSettingsComponents(newPanelView(checkGuildID, categoryGeneral, "manychoices", checkToken, true))
	menu := rowMenu(t, components[len(components)-1])

	if len(menu.Options) != selectOptionLimit {
		t.Errorf("the value list offers %d options, want %d", len(menu.Options), selectOptionLimit)
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

func TestNextValueLeavesTextSettingsUnchanged(t *testing.T) {
	if got := nextValue(specFor(t, "prefix"), "abc"); got != "abc" {
		t.Errorf("nextValue = %q for a text setting, want it unchanged", got)
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
