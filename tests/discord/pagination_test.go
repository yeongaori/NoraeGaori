package discord_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
)

func TestPageCountNeverDropsBelowOne(t *testing.T) {
	for _, check := range []struct{ items, perPage, want int }{
		{0, 10, 1}, {10, 10, 1}, {11, 10, 2}, {25, 10, 3},
	} {
		if got := discord.PageCount(check.items, check.perPage); got != check.want {
			t.Errorf("PageCount(%d, %d) = %d, want %d", check.items, check.perPage, got, check.want)
		}
	}
}

func TestClampPageKeepsPagesInRange(t *testing.T) {
	for _, check := range []struct{ page, totalPages, want int }{
		{0, 3, 1}, {-4, 3, 1}, {2, 3, 2}, {9, 3, 3}, {5, 0, 1},
	} {
		if got := discord.ClampPage(check.page, check.totalPages); got != check.want {
			t.Errorf("ClampPage(%d, %d) = %d, want %d", check.page, check.totalPages, got, check.want)
		}
	}
}

func TestPageBoundsStayInsideTheItems(t *testing.T) {
	for _, check := range []struct{ page, items, start, end int }{
		{1, 12, 0, 5}, {3, 12, 10, 12}, {4, 12, 0, 0}, {0, 12, 0, 0}, {1, 0, 0, 0}, {1 << 62, 12, 0, 0},
	} {
		start, end := discord.PageBounds(check.page, 5, check.items)
		if start != check.start || end != check.end {
			t.Errorf("PageBounds(%d, 5, %d) = (%d, %d), want (%d, %d)", check.page, check.items, start, end, check.start, check.end)
		}
	}
}

func pageButtons(t *testing.T, row discordgo.ActionsRow) []discordgo.Button {
	t.Helper()

	buttons := make([]discordgo.Button, 0, len(row.Components))
	for _, component := range row.Components {
		button, ok := component.(discordgo.Button)
		if !ok {
			t.Fatalf("row holds %T, want only buttons", component)
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func TestPageButtonRowPointsAtTheNeighbouringPages(t *testing.T) {
	arguments := []string{"admin"}
	buttons := pageButtons(t, discord.PageButtonRow("help_page", 2, 3, "Previous", "Next", arguments, discordgo.Button{CustomID: "extra"}))

	want := []string{"help_page:admin:1", "help_page:admin:3", "extra"}
	if len(buttons) != len(want) {
		t.Fatalf("built %d buttons, want %d", len(buttons), len(want))
	}
	for index, button := range buttons {
		if button.CustomID != want[index] {
			t.Errorf("button %d routes to %q, want %q", index, button.CustomID, want[index])
		}
		if button.Disabled {
			t.Errorf("button %d is disabled on a middle page", index)
		}
	}
	if len(arguments) != 1 || arguments[0] != "admin" {
		t.Errorf("the caller's arguments changed to %v", arguments)
	}

	if buttons[0].Label != "Previous" || buttons[1].Label != "Next" {
		t.Errorf("labels = %q, %q, want Previous then Next", buttons[0].Label, buttons[1].Label)
	}

	for name, check := range map[string]struct {
		page                     int
		isPreviousOff, isNextOff bool
	}{
		"the first of three": {1, true, false},
		"the last of three":  {3, false, true},
	} {
		row := pageButtons(t, discord.PageButtonRow("queue_page", check.page, 3, "Previous", "Next", nil))
		if row[0].Disabled != check.isPreviousOff || row[1].Disabled != check.isNextOff {
			t.Errorf("%s: previous and next disabled = %v, %v, want %v, %v", name, row[0].Disabled, row[1].Disabled, check.isPreviousOff, check.isNextOff)
		}
	}

	edges := pageButtons(t, discord.PageButtonRow("queue_page", 1, 1, "Previous", "Next", nil))
	if len(edges) != 2 || edges[0].CustomID != "queue_page:0" || edges[1].CustomID != "queue_page:2" {
		t.Fatalf("single-page buttons = %+v", edges)
	}
	if !edges[0].Disabled || !edges[1].Disabled {
		t.Error("the buttons of a single page are not both disabled")
	}
}
