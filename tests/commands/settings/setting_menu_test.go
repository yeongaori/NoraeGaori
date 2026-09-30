package settings_test

import (
	"errors"
	"testing"

	"noraegaori/internal/commands/settings"
	"noraegaori/tests/testutil/dbtest"
)

var dropdownSettingKeys = []string{
	"repeat", "sponsorblock", "showstartedtrack", "normalization",
	"fadein", "fadeout", "automix", "crossfade", "fadeonstop", "trimsilence",
	"autoleave", "autopause", "autoresume",
}

func TestEverySettingCommandHasAMenuSpec(t *testing.T) {
	for _, key := range dropdownSettingKeys {
		spec := specFor(t, key)
		if !settings.HookHasSettingMenu(spec) {
			t.Errorf("%s has no dropdown menu", key)
		}
	}
}

func TestInputAndAdminSettingsGetNoMenu(t *testing.T) {
	for _, key := range []string{"prefix", "language", "volume", "fadein_duration", "automix_beats"} {
		spec := specFor(t, key)
		if settings.HookHasSettingMenu(spec) {
			t.Errorf("%s was given a dropdown menu", key)
		}
	}
}

func TestSettingMenusShowTheStoredValueAndAcceptRepeatedPicks(t *testing.T) {
	dbtest.Setup(t)

	for _, key := range dropdownSettingKeys {
		spec := specFor(t, key)
		values := settings.HookSettingMenuValues(spec)

		menu := settings.HookBuildSettingMenu(checkGuildID, spec)
		if menu.Label != settings.HookSettingLabel(checkGuildID, key) {
			t.Errorf("%s dropdown label = %q", key, menu.Label)
		}
		if len(menu.Options) != len(values) {
			t.Fatalf("%s dropdown offers %d options, want %d", key, len(menu.Options), len(values))
		}

		for _, picked := range append(append([]string{}, values...), values[0]) {
			if embed, err := menu.Apply(picked); err != nil || embed != nil {
				t.Fatalf("%s pick %q = (%v, %v), want a clean apply", key, picked, embed, err)
			}

			stored, ok := settings.HookCurrentValue(checkGuildID, spec)
			if !ok || stored != picked {
				t.Errorf("%s pick %q stored %q", key, picked, stored)
			}

			refreshed := settings.HookBuildSettingMenu(checkGuildID, spec)
			if refreshed.Current != settings.HookFormatSettingValue(checkGuildID, spec, picked) {
				t.Errorf("%s after picking %q shows %q", key, picked, refreshed.Current)
			}
			defaults := 0
			for _, option := range refreshed.Options {
				if option.Default {
					defaults++
					if option.Value != picked {
						t.Errorf("%s after picking %q preselects %q", key, picked, option.Value)
					}
				}
			}
			if defaults != 1 {
				t.Errorf("%s after picking %q preselects %d options, want 1", key, picked, defaults)
			}
		}
	}
}

func TestSettingMenuRejectsAnUnknownPick(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "sponsorblock")
	embed, err := settings.HookBuildSettingMenu(checkGuildID, spec).Apply("maybe")
	if !errors.Is(err, *settings.HookErrUnknownValue) || embed == nil {
		t.Errorf("unknown pick = (%v, %v), want an error embed and errUnknownValue", embed, err)
	}
}

func TestThePanelShowsUnavailableForAnUnreadableSetting(t *testing.T) {
	dbtest.Setup(t)
	dbtest.CloseUntilCleanup(t)

	spec := specFor(t, "sponsorblock")
	view := settings.HookNewPanelView(checkGuildID, *spec.HookCategory(), true)
	if got := view.HookDisplayValue(spec); got != settings.HookPanelStrings(checkGuildID).ReadFailed {
		t.Errorf("an unreadable setting displays %q, want %q", got, settings.HookPanelStrings(checkGuildID).ReadFailed)
	}
}
