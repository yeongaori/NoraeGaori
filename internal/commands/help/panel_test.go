package help

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/internal/discord/command"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/configtest"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
)

const helpCheckGuildID = "help-guild"

func setupHelpCommands(t *testing.T) {
	t.Helper()

	configtest.Setup(t)
	dbtest.Setup(t)
	for index := range 11 {
		command.RegisterCommand(&command.Command{Name: fmt.Sprintf("helpcheck%02d", index), Description: "d", AdminOnly: index == 0})
	}
}

func helpButtons(t *testing.T, components []discordgo.MessageComponent) []discordgo.Button {
	t.Helper()

	if len(components) != 1 {
		t.Fatalf("built %d rows, want one button row", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("the row is %T, want an action row", components[0])
	}
	buttons := make([]discordgo.Button, 0, len(row.Components))
	for _, component := range row.Components {
		button, ok := component.(discordgo.Button)
		if !ok {
			t.Fatalf("component %T is not a button", component)
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func paginationFooter(page, totalPages int) string {
	return fmt.Sprintf(messages.T(helpCheckGuildID).Footers.HelpPagination, page, totalPages)
}

func TestHelpPagesClampAndKeepTheOpenersView(t *testing.T) {
	setupHelpCommands(t)

	for _, check := range []struct {
		isAdmin    bool
		totalPages int
	}{{true, 3}, {false, 2}} {
		embed, components, hasCommands := buildHelpPage(helpCheckGuildID, check.isAdmin, 99)
		if !hasCommands {
			t.Fatalf("admin=%v found no commands", check.isAdmin)
		}
		if embed.Footer.Text != paginationFooter(check.totalPages, check.totalPages) {
			t.Errorf("admin=%v footer = %q, want the clamped last page", check.isAdmin, embed.Footer.Text)
		}

		buttons := helpButtons(t, components)
		wantPrevious := discord.ComponentID(helpPageRoute, discord.ViewArgument(check.isAdmin), strconv.Itoa(check.totalPages-1))
		if len(buttons) != 2 || buttons[0].CustomID != wantPrevious || !buttons[1].Disabled {
			t.Errorf("admin=%v buttons = %+v, want previous %q and a disabled next", check.isAdmin, buttons, wantPrevious)
		}
	}
}

func TestTurningAHelpPageRedrawsTheMessage(t *testing.T) {
	setupHelpCommands(t)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:      "111",
		AppID:   "app",
		Token:   "token",
		Type:    discordgo.InteractionMessageComponent,
		GuildID: helpCheckGuildID,
		Data:    discordgo.MessageComponentInteractionData{CustomID: helpPageRoute},
	}}

	for _, ignored := range [][]string{{"2"}, {"owner", "2"}, {"admin", "two"}} {
		turnHelpPage(session, ic, ignored)
	}
	turnHelpPage(session, ic, []string{discord.ViewArgument(true), "2"})

	sent := requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want one redraw", len(sent))
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "type"); got != float64(discordgo.InteractionResponseUpdateMessage) {
		t.Errorf("reply type = %v, want an in-place update", got)
	}
	if got := discordtest.JSONAt(t, sent[0].Body, "data", "embeds", 0, "footer", "text"); got != paginationFooter(2, 3) {
		t.Errorf("footer = %v, want page 2 of 3", got)
	}
}
