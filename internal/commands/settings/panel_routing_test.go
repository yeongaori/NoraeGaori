package settings

import (
	"testing"

	"noraegaori/internal/discord"
	"noraegaori/internal/testutil/dbtest"
)

func TestPanelArgumentsCarryTheViewCategoryAndKey(t *testing.T) {
	checks := []struct {
		arguments []string
		want      panelTarget
	}{
		{[]string{"admin"}, panelTarget{isAdmin: true}},
		{[]string{"member", categoryMixing}, panelTarget{category: categoryMixing}},
		{[]string{"admin", categoryGeneral, "language"}, panelTarget{isAdmin: true, category: categoryGeneral, key: "language"}},
	}

	for _, check := range checks {
		got, isValid := parsePanelArguments(check.arguments)
		if !isValid || got != check.want {
			t.Errorf("parsePanelArguments(%v) = (%+v, %v), want %+v", check.arguments, got, isValid, check.want)
		}
	}
}

func TestPanelArgumentsRejectUnknownViewsAndHiddenCategories(t *testing.T) {
	for name, arguments := range map[string][]string{
		"nothing":             nil,
		"an unknown view":     {"owner", categoryMixing},
		"an unknown category": {"admin", "nonsense"},
		"a hidden category":   {"member", categoryGeneral},
		"too many parts":      {"admin", categoryMixing, "fadein", "extra"},
	} {
		if _, isValid := parsePanelArguments(arguments); isValid {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestPanelComponentsRouteBackToTheirView(t *testing.T) {
	dbtest.Setup(t)

	for _, isAdmin := range []bool{true, false} {
		view := newPanelView(checkGuildID, defaultCategory(isAdmin), isAdmin)
		components := buildSettingsComponents(view)

		if got := rowMenu(t, components[0]).CustomID; got != discord.ComponentID(categoryRoute, discord.ViewArgument(isAdmin)) {
			t.Errorf("admin=%v category select routes to %q", isAdmin, got)
		}
		if got := rowMenu(t, components[1]).CustomID; got != discord.ComponentID(pickRoute, discord.ViewArgument(isAdmin), view.category) {
			t.Errorf("admin=%v setting select routes to %q", isAdmin, got)
		}
	}
}
