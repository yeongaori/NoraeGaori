package queue

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestQueueButtonsCarryThePanelToken(t *testing.T) {
	components := createQueueButtons("guild", 1, 3, "tok")

	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("the first component is %T, want an action row", components[0])
	}
	if len(row.Components) != 3 {
		t.Fatalf("the row holds %d buttons, want 3", len(row.Components))
	}

	for _, component := range row.Components {
		button, ok := component.(discordgo.Button)
		if !ok {
			t.Fatalf("component %T is not a button", component)
		}
		if !strings.HasSuffix(button.CustomID, "_tok") {
			t.Errorf("button custom id %q does not carry the panel token", button.CustomID)
		}
	}
}

func TestTheQueuePanelOnlyTurnsOnItsOwnButtons(t *testing.T) {
	panel := &queuePanel{guildID: "guild", token: "tok", perPage: 10, page: 2}

	for _, customID := range []string{
		queueNextPrefix + "other-token",
		queuePrevPrefix + "other-token",
		"queue_next",
		"help_next_tok",
		"",
	} {
		if panel.isPageButton(customID) {
			t.Errorf("the panel claimed a foreign custom id %q", customID)
		}
		if page := panel.turnPage(customID, 3); page != 2 {
			t.Errorf("a foreign custom id %q moved the panel to page %d, want 2", customID, page)
		}
	}

	if page := panel.turnPage(queueNextPrefix+"tok", 3); page != 3 {
		t.Errorf("its own next button gave page %d, want 3", page)
	}
	if page := panel.turnPage(queueNextPrefix+"tok", 3); page != 3 {
		t.Errorf("next past the last page gave %d, want it clamped to 3", page)
	}
	if page := panel.turnPage(queuePrevPrefix+"tok", 3); page != 2 {
		t.Errorf("its own previous button gave page %d, want 2", page)
	}
}

func TestTheQueuePanelFollowsAShrinkingQueue(t *testing.T) {
	panel := &queuePanel{guildID: "guild", token: "tok", perPage: 10, page: 3}

	if got := panel.pageCount(25); got != 3 {
		t.Errorf("25 songs span %d pages, want 3", got)
	}
	if got := panel.pageCount(10); got != 1 {
		t.Errorf("10 songs span %d pages, want 1", got)
	}
	if got := panel.pageCount(0); got != 1 {
		t.Errorf("an empty queue spans %d pages, want 1", got)
	}

	if page := panel.currentPage(panel.pageCount(5)); page != 1 {
		t.Errorf("after the queue shrank to one page the panel sits on page %d, want 1", page)
	}
	if page := panel.turnPage(queueNextPrefix+"tok", 1); page != 1 {
		t.Errorf("next on a single-page queue gave %d, want 1", page)
	}
}
