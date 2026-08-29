package help

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestHelpButtonsCarryThePanelToken(t *testing.T) {
	components := createHelpButtons("guild", 1, 3, "tok")

	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("the first component is %T, want an action row", components[0])
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

func TestTheHelpPanelOnlyTurnsOnItsOwnButtons(t *testing.T) {
	panel := &helpPanel{guildID: "guild", token: "tok", totalPages: 3, page: 2}

	for _, customID := range []string{
		helpNextPrefix + "other-token",
		helpPrevPrefix + "other-token",
		"help_next",
		"queue_next_tok",
		"",
	} {
		if _, turned := panel.turnPage(customID); turned {
			t.Errorf("the panel turned on a foreign custom id %q", customID)
		}
	}

	if page, turned := panel.turnPage(helpNextPrefix + "tok"); !turned || page != 3 {
		t.Errorf("its own next button gave page %d turned=%v, want page 3", page, turned)
	}
	if page, turned := panel.turnPage(helpNextPrefix + "tok"); !turned || page != 3 {
		t.Errorf("next past the last page gave %d, want it clamped to 3", page)
	}
	if page, turned := panel.turnPage(helpPrevPrefix + "tok"); !turned || page != 2 {
		t.Errorf("its own previous button gave page %d turned=%v, want page 2", page, turned)
	}
}

func TestTheHelpPanelRendersOnlyItsOwnPage(t *testing.T) {
	commands := make([]CommandInfo, 7)
	for index := range commands {
		commands[index] = CommandInfo{Name: "cmd", Description: "d", Usage: "u", Example: "e"}
	}
	panel := &helpPanel{guildID: "guild", token: "tok", totalPages: 2, perPage: 5, commands: commands, page: 2}

	embed, components := panel.render(2)
	if embed == nil {
		t.Fatal("the last page rendered no embed")
	}
	if len(components) == 0 {
		t.Fatal("the last page rendered no components")
	}
}
