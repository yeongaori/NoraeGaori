package settings

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/dbtest"
)

const checkGuildID = "settings-panel-guild"

const discordRowLimit = 5

func rowMenu(t *testing.T, row discordgo.MessageComponent) discordgo.SelectMenu {
	t.Helper()

	actionsRow, ok := row.(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("panel row is %T, want discordgo.ActionsRow", row)
	}
	if len(actionsRow.Components) != 1 {
		t.Fatalf("panel row holds %d components, want a single select menu", len(actionsRow.Components))
	}
	menu, ok := actionsRow.Components[0].(discordgo.SelectMenu)
	if !ok {
		t.Fatalf("panel row holds %T, want discordgo.SelectMenu", actionsRow.Components[0])
	}
	return menu
}

func TestEveryCategoryStaysWithinDiscordComponentLimits(t *testing.T) {
	dbtest.Setup(t)

	for _, category := range settingCategories {
		for _, isAdmin := range []bool{true, false} {
			components := buildSettingsComponents(checkGuildID, category, "", "token", isAdmin)

			if len(components) > discordRowLimit {
				t.Errorf("category %q (admin=%v) built %d rows, want at most %d",
					category, isAdmin, len(components), discordRowLimit)
			}
			for index, row := range components {
				menu := rowMenu(t, row)
				if len(menu.Options) == 0 {
					t.Errorf("category %q (admin=%v) row %d has an empty menu", category, isAdmin, index)
				}
				if len(menu.Options) > selectOptionLimit {
					t.Errorf("category %q (admin=%v) row %d offers %d options, want at most %d",
						category, isAdmin, index, len(menu.Options), selectOptionLimit)
				}
			}
		}
	}
}

func TestEveryCategoryShowsExactlyTwoRows(t *testing.T) {
	dbtest.Setup(t)

	for _, category := range settingCategories {
		for _, isAdmin := range []bool{true, false} {
			if len(settingsInCategory(category, isAdmin)) == 0 {
				continue
			}
			components := buildSettingsComponents(checkGuildID, category, "", "token", isAdmin)
			if len(components) != 2 {
				t.Errorf("category %q (admin=%v) built %d rows, want 2", category, isAdmin, len(components))
			}
		}
	}

	open := buildSettingsComponents(checkGuildID, categoryGeneral, "language", "token", true)
	if len(open) != 2 {
		t.Errorf("the open language view built %d rows, want 2", len(open))
	}
}

func TestThePickerListsEverySettingWithItsCurrentValue(t *testing.T) {
	dbtest.Setup(t)

	for _, category := range settingCategories {
		specs := settingsInCategory(category, true)
		if len(specs) == 0 {
			continue
		}

		components := buildSettingsComponents(checkGuildID, category, "", "token", true)
		menu := rowMenu(t, components[len(components)-1])

		if menu.CustomID != pickPrefix+"token" {
			t.Fatalf("category %q picker has custom id %q", category, menu.CustomID)
		}
		if len(menu.Options) != len(specs) {
			t.Fatalf("category %q picker lists %d settings, want %d", category, len(menu.Options), len(specs))
		}
		for index, spec := range specs {
			option := menu.Options[index]
			if option.Value != spec.key {
				t.Errorf("picker option %d is %q, want %q", index, option.Value, spec.key)
			}
			if option.Label != settingLabel(checkGuildID, spec.key) {
				t.Errorf("picker option %q is labelled %q", spec.key, option.Label)
			}
			if option.Description != displayValue(checkGuildID, spec) {
				t.Errorf("picker option %q describes %q, want the current value %q",
					spec.key, option.Description, displayValue(checkGuildID, spec))
			}
			if option.Default {
				t.Errorf("picker option %q is marked selected, which stops it firing when picked again", spec.key)
			}
		}
	}
}

func TestAdminOnlySettingsAreHiddenFromNonAdmins(t *testing.T) {
	dbtest.Setup(t)

	for _, spec := range settingSpecs {
		if !spec.adminOnly {
			continue
		}
		for _, visible := range settingsInCategory(spec.category, false) {
			if visible.key == spec.key {
				t.Errorf("admin-only setting %q is visible to a non-admin", spec.key)
			}
		}
	}

	if categories := visibleCategories(false); len(categories) == 0 {
		t.Fatal("a non-admin sees no categories at all")
	}
	for _, category := range visibleCategories(false) {
		if category == categoryGeneral {
			t.Error("the general category is offered to a non-admin but holds only admin settings")
		}
	}
}

func TestNonAdminPanelDefaultsToAVisibleCategory(t *testing.T) {
	dbtest.Setup(t)

	category := defaultCategory(false)
	if len(settingsInCategory(category, false)) == 0 {
		t.Errorf("default category %q is empty for a non-admin", category)
	}
	if admin := defaultCategory(true); admin != categoryGeneral {
		t.Errorf("an admin panel opens on %q, want %q", admin, categoryGeneral)
	}
}

func TestEmbedListsEveryVisibleSettingInTheCategory(t *testing.T) {
	dbtest.Setup(t)

	for _, category := range settingCategories {
		embed := buildSettingsEmbed(checkGuildID, category, true)
		specs := settingsInCategory(category, true)

		if len(embed.Fields) != len(specs) {
			t.Errorf("category %q shows %d fields, want %d", category, len(embed.Fields), len(specs))
			continue
		}
		for index, spec := range specs {
			want := settingLabel(checkGuildID, spec.key)
			if embed.Fields[index].Name != want {
				t.Errorf("category %q field %d is %q, want %q", category, index, embed.Fields[index].Name, want)
			}
			if embed.Fields[index].Value == "" {
				t.Errorf("setting %q renders an empty value", spec.key)
			}
		}
	}
}

func TestTogglingASettingPersistsAndShowsTheNewValue(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "sponsorblock")

	before, ok := currentValue(checkGuildID, spec)
	if !ok {
		t.Fatal("could not read sponsorblock")
	}

	if err := applySetting(checkGuildID, spec, nextValue(spec, before)); err != nil {
		t.Fatalf("failed to toggle sponsorblock: %v", err)
	}

	after, ok := currentValue(checkGuildID, spec)
	if !ok {
		t.Fatal("could not re-read sponsorblock")
	}
	if after == before {
		t.Errorf("sponsorblock stayed %q after a toggle", before)
	}
	if displayValue(checkGuildID, spec) == "" {
		t.Error("sponsorblock renders an empty value after a toggle")
	}
}

func TestRejectedModalValuesLeaveTheSettingUntouched(t *testing.T) {
	dbtest.Setup(t)

	spec := specFor(t, "volume")

	if err := applySetting(checkGuildID, spec, "42"); err != nil {
		t.Fatalf("failed to set volume: %v", err)
	}

	for _, value := range []string{"abc", "5000", "-1"} {
		if err := applySetting(checkGuildID, spec, value); err == nil {
			t.Errorf("volume accepted %q", value)
		}
	}

	after, ok := currentValue(checkGuildID, spec)
	if !ok {
		t.Fatal("could not read volume")
	}
	if after != "42" {
		t.Errorf("volume is %q after rejected writes, want \"42\"", after)
	}
}

func TestOpeningLanguageReplacesThePickerWithItsValues(t *testing.T) {
	dbtest.Setup(t)

	components := buildSettingsComponents(checkGuildID, categoryGeneral, "language", "token", true)
	menu := rowMenu(t, components[1])

	if menu.CustomID != customID(choicePrefix, "language", "token") {
		t.Fatalf("the open language row has custom id %q", menu.CustomID)
	}
	if menu.Options[0].Value != backValue {
		t.Errorf("the first option is %q, want the back entry %q", menu.Options[0].Value, backValue)
	}
	if menu.Options[1].Value != defaultChoiceValue {
		t.Errorf("the second option is %q, want %q", menu.Options[1].Value, defaultChoiceValue)
	}
	if !menu.Options[1].Default {
		t.Error("an unset language does not mark the default option as selected")
	}

	for _, code := range messages.AvailableLocales() {
		found := false
		for _, option := range menu.Options {
			if option.Value == code {
				found = true
			}
		}
		if !found {
			t.Errorf("locale %q is missing from the language list", code)
		}
	}
}

func TestAnUnknownOrNonChoiceOpenSettingFallsBackToThePicker(t *testing.T) {
	dbtest.Setup(t)

	for _, open := range []string{"no-such-setting", "prefix", "sponsorblock"} {
		components := buildSettingsComponents(checkGuildID, categoryGeneral, open, "token", true)
		menu := rowMenu(t, components[1])

		if menu.CustomID != pickPrefix+"token" {
			t.Errorf("open=%q rendered %q, want the picker", open, menu.CustomID)
		}
	}
}
