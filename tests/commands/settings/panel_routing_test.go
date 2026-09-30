package settings_test

import (
	"testing"

	"noraegaori/internal/commands/settings"
	"noraegaori/internal/discord"
	"noraegaori/tests/testutil/dbtest"
)

func TestPanelArgumentsCarryTheViewCategoryAndKey(t *testing.T) {
	checks := []struct {
		arguments []string
		want      settings.HookPanelTarget
	}{
		{[]string{"admin"}, *settings.HookBuildPanelTarget(settings.HookPanelTargetFields{IsAdmin: true})},
		{[]string{"member", settings.HookCategoryMixing}, *settings.HookBuildPanelTarget(settings.HookPanelTargetFields{Category: settings.HookCategoryMixing})},
		{[]string{"admin", settings.HookCategoryGeneral, "language"}, *settings.HookBuildPanelTarget(settings.HookPanelTargetFields{IsAdmin: true, Category: settings.HookCategoryGeneral, Key: "language"})},
	}

	for _, check := range checks {
		got, isValid := settings.HookParsePanelArguments(check.arguments)
		if !isValid || got != check.want {
			t.Errorf("parsePanelArguments(%v) = (%+v, %v), want %+v", check.arguments, got, isValid, check.want)
		}
	}
}

func TestPanelArgumentsRejectUnknownViewsAndHiddenCategories(t *testing.T) {
	for name, arguments := range map[string][]string{
		"nothing":             nil,
		"an unknown view":     {"owner", settings.HookCategoryMixing},
		"an unknown category": {"admin", "nonsense"},
		"a hidden category":   {"member", settings.HookCategoryGeneral},
		"too many parts":      {"admin", settings.HookCategoryMixing, "fadein", "extra"},
	} {
		if _, isValid := settings.HookParsePanelArguments(arguments); isValid {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestPanelComponentsRouteBackToTheirView(t *testing.T) {
	dbtest.Setup(t)

	for _, isAdmin := range []bool{true, false} {
		view := settings.HookNewPanelView(checkGuildID, settings.HookDefaultCategory(isAdmin), isAdmin)
		components := settings.HookBuildSettingsComponents(view)

		if got := rowMenu(t, components[0]).CustomID; got != discord.ComponentID(settings.HookCategoryRoute, discord.ViewArgument(isAdmin)) {
			t.Errorf("admin=%v category select routes to %q", isAdmin, got)
		}
		if got := rowMenu(t, components[1]).CustomID; got != discord.ComponentID(settings.HookPickRoute, discord.ViewArgument(isAdmin), *view.HookCategory()) {
			t.Errorf("admin=%v setting select routes to %q", isAdmin, got)
		}
	}
}
