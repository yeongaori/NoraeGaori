package queue_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	queuecommand "noraegaori/internal/commands/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/queuetest"
)

func queueButtons(t *testing.T, page, totalPages int) []discordgo.Button {
	t.Helper()

	components := queuecommand.HookCreateQueueButtons("guild", page, totalPages)
	if len(components) != 1 {
		t.Fatalf("built %d rows, want one button row", len(components))
	}
	row, isRow := components[0].(discordgo.ActionsRow)
	if !isRow {
		t.Fatalf("the row is %T, want an action row", components[0])
	}
	buttons := make([]discordgo.Button, 0, len(row.Components))
	for _, component := range row.Components {
		button, isButton := component.(discordgo.Button)
		if !isButton {
			t.Fatalf("component %T is not a button", component)
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func TestQueueButtonsPageAndOpenTheMixPanel(t *testing.T) {
	buttons := queueButtons(t, 2, 3)

	want := []string{queuecommand.HookQueuePageRoute + ":1", queuecommand.HookQueuePageRoute + ":3", queuecommand.HookQueueMixRoute}
	if len(buttons) != len(want) {
		t.Fatalf("the row holds %d buttons, want %d", len(buttons), len(want))
	}
	for index, button := range buttons {
		if button.CustomID != want[index] {
			t.Errorf("button %d routes to %q, want %q", index, button.CustomID, want[index])
		}
		if button.Disabled {
			t.Errorf("button %q is disabled on a middle page", button.CustomID)
		}
	}
}

func TestQueueButtonsStopAtTheFirstAndLastPage(t *testing.T) {
	for name, check := range map[string]struct {
		page, totalPages         int
		isPreviousOff, isNextOff bool
	}{
		"the first page": {1, 3, true, false},
		"the last page":  {3, 3, false, true},
		"the only page":  {1, 1, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			buttons := queueButtons(t, check.page, check.totalPages)
			if buttons[0].Disabled != check.isPreviousOff || buttons[1].Disabled != check.isNextOff {
				t.Errorf("previous and next disabled = %v, %v, want %v, %v", buttons[0].Disabled, buttons[1].Disabled, check.isPreviousOff, check.isNextOff)
			}
			if buttons[2].Disabled {
				t.Error("the mix button was disabled")
			}
		})
	}
}

func TestQueuePagesListTheirOwnSongsWithQueuePositions(t *testing.T) {
	songs := queuetest.SongsBy(commandtest.CallerID, 25)

	first, _ := queuecommand.HookRenderQueuePage("guild", songs, 1)
	for _, want := range []string{"▶️ **[Song 1]", "2. **[Song 2]", "10. **[Song 10]"} {
		if !strings.Contains(first.Description, want) {
			t.Errorf("the first page lacks %q:\n%s", want, first.Description)
		}
	}
	if strings.Contains(first.Description, "Song 11]") {
		t.Error("the first page also lists song 11")
	}

	last, _ := queuecommand.HookRenderQueuePage("guild", songs, 9)
	for index := 21; index <= 25; index++ {
		if want := fmt.Sprintf("%d. **[Song %d]", index, index); !strings.Contains(last.Description, want) {
			t.Errorf("the clamped last page lacks %q:\n%s", want, last.Description)
		}
	}
	if strings.Contains(last.Description, "▶️") || strings.Contains(last.Description, "Song 20]") {
		t.Errorf("the last page shows songs from an earlier page:\n%s", last.Description)
	}
}

func TestQueueRoutesIgnoreMalformedArguments(t *testing.T) {
	fixture := commandtest.NewFixture(t, nil, queuetest.SongsBy(commandtest.CallerID, 25)...)
	component := fixture.Component(queuecommand.HookQueuePageRoute)
	modal := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionModalSubmit, GuildID: commandtest.GuildID}}

	queuecommand.HookTurnQueuePage(fixture.Session, component, nil)
	queuecommand.HookTurnQueuePage(fixture.Session, component, []string{"two"})
	queuecommand.HookTurnQueuePage(fixture.Session, modal, []string{"2"})
	queuecommand.HookOpenMixPanel(fixture.Session, modal, nil)

	fixture.WantNoRequests(t)
}
