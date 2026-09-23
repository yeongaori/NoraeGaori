package queue

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func TestQueueButtonsPageAndOpenTheMixPanel(t *testing.T) {
	components := createQueueButtons("guild", 2, 3)
	if len(components) != 1 {
		t.Fatalf("built %d rows, want one button row", len(components))
	}
	row, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("the row is %T, want an action row", components[0])
	}

	want := []string{queuePageRoute + ":1", queuePageRoute + ":3", queueMixRoute}
	if len(row.Components) != len(want) {
		t.Fatalf("the row holds %d buttons, want %d", len(row.Components), len(want))
	}
	for index, component := range row.Components {
		button, ok := component.(discordgo.Button)
		if !ok {
			t.Fatalf("component %T is not a button", component)
		}
		if button.CustomID != want[index] {
			t.Errorf("button %d routes to %q, want %q", index, button.CustomID, want[index])
		}
		if button.Disabled {
			t.Errorf("button %q is disabled on a middle page", button.CustomID)
		}
	}
}

func TestQueuePagesClampToTheCurrentQueue(t *testing.T) {
	songs := make([]*queue.Song, 25)
	for index := range songs {
		songs[index] = &queue.Song{Title: fmt.Sprintf("song %d", index)}
	}

	embed, _ := renderQueuePage("guild", songs, 9)

	if want := fmt.Sprintf(messages.T("guild").Footers.Pagination, 3, 3, len(songs)); embed.Footer.Text != want {
		t.Errorf("footer = %q, want %q", embed.Footer.Text, want)
	}
}

func TestQueueRoutesIgnoreMalformedArguments(t *testing.T) {
	component := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionMessageComponent, GuildID: "guild"}}
	modal := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionModalSubmit, GuildID: "guild"}}

	turnQueuePage(nil, component, nil)
	turnQueuePage(nil, component, []string{"two"})
	turnQueuePage(nil, modal, []string{"2"})
	openMixPanel(nil, modal, nil)
}
