package automix_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/commands/automix"
	"noraegaori/internal/queue"
)

func checkSong(id int, title string) *queue.Song {
	return &queue.Song{
		ID:    id,
		Title: title,
		URL:   fmt.Sprintf("https://example.invalid/watch?v=%d", id),
	}
}

func checkSongs(count int, title string) []*queue.Song {
	songs := make([]*queue.Song, 0, count)
	for i := 0; i < count; i++ {
		songs = append(songs, checkSong(i+1, fmt.Sprintf("%s %d", title, i+1)))
	}
	return songs
}

func checkPanelState(songs []*queue.Song, guildOverrides map[string]string, autoSelect bool) *automix.HookPanelState {
	return automix.HookBuildPanelState(automix.HookPanelStateFields{
		Pairs:          automix.HookTransitionPairs(songs),
		GuildOverrides: guildOverrides,
		AutoSelect:     autoSelect,
		Crossfade:      true,
		AutoMixBeats:   16,
		CrossfadeSec:   8,
		BackfillActive: true,
	})
}

func checkRowsFor(songs []*queue.Song) []*automix.HookTransitionRow {
	return checkRowsWithGuild(songs, nil)
}

func checkRowsWithGuild(songs []*queue.Song, guildOverrides map[string]string) []*automix.HookTransitionRow {
	state := checkPanelState(songs, guildOverrides, true)
	return automix.HookHydrateTransitionRows("check-guild", state, *state.HookPairs())
}

func editorTab(key string) *automix.HookEditorTab {
	return automix.HookFindTab(key)
}

func inspectComponents(components []discordgo.MessageComponent) (rows int, selects []discordgo.SelectMenu, buttons []discordgo.Button) {
	for _, component := range components {
		row, ok := component.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		rows++
		for _, inner := range row.Components {
			switch typed := inner.(type) {
			case discordgo.SelectMenu:
				selects = append(selects, typed)
			case discordgo.Button:
				buttons = append(buttons, typed)
			}
		}
	}
	return rows, selects, buttons
}

func checkSelectMenu(context string, menu discordgo.SelectMenu) []string {
	violations := []string{}

	if len([]rune(menu.CustomID)) > automix.HookDiscordLabelLimit {
		violations = append(violations, fmt.Sprintf("%s: custom id %d chars", context, len([]rune(menu.CustomID))))
	}
	if len(menu.Options) == 0 {
		violations = append(violations, fmt.Sprintf("%s: no options", context))
	}
	if len(menu.Options) > automix.HookDiscordSelectLimit {
		violations = append(violations, fmt.Sprintf("%s: %d options", context, len(menu.Options)))
	}
	if len([]rune(menu.Placeholder)) > 150 {
		violations = append(violations, fmt.Sprintf("%s: placeholder %d chars", context, len([]rune(menu.Placeholder))))
	}

	seen := map[string]bool{}
	defaults := 0
	for _, option := range menu.Options {
		if label := []rune(option.Label); len(label) > automix.HookDiscordLabelLimit || len(label) == 0 {
			violations = append(violations, fmt.Sprintf("%s: label %d chars", context, len(label)))
		}
		if len([]rune(option.Description)) > automix.HookDiscordLabelLimit {
			violations = append(violations, fmt.Sprintf("%s: description %d chars", context, len([]rune(option.Description))))
		}
		if value := []rune(option.Value); len(value) > automix.HookDiscordLabelLimit || len(value) == 0 {
			violations = append(violations, fmt.Sprintf("%s: value %d chars", context, len(value)))
		}
		if seen[option.Value] {
			violations = append(violations, fmt.Sprintf("%s: duplicate value %q", context, option.Value))
		}
		seen[option.Value] = true
		if option.Default {
			defaults++
		}
	}
	if defaults > 1 {
		violations = append(violations, fmt.Sprintf("%s: %d default options", context, defaults))
	}

	return violations
}

func checkEditor(t *testing.T, row *automix.HookTransitionRow, tab *automix.HookEditorTab) {
	t.Helper()
	components := automix.HookCreateTransitionEditorComponents("check-guild", row, tab, checkLocation)
	rowCount, selects, buttons := inspectComponents(components)

	if rowCount > 5 {
		t.Errorf("%s tab has %d action rows, want at most 5", tab.HookKey(), rowCount)
	}
	if len(buttons) != len(*automix.HookEditorTabs) {
		t.Errorf("%s tab has %d buttons, want one per tab", tab.HookKey(), len(buttons))
	}
	for _, button := range buttons {
		if size := len([]rune(button.CustomID)); size > automix.HookDiscordLabelLimit {
			t.Errorf("tab button custom id is %d chars, want at most %d", size, automix.HookDiscordLabelLimit)
		}
	}
	for _, menu := range selects {
		for _, violation := range checkSelectMenu(tab.HookKey()+" tab", menu) {
			t.Error(violation)
		}
	}

	embed := automix.HookCreateTransitionEditorEmbed("check-guild", row, tab, "")
	if size := len([]rune(embed.Title)); size > 256 {
		t.Errorf("editor title is %d chars, want at most 256", size)
	}
	for _, field := range embed.Fields {
		if size := len([]rune(field.Name)); size > 256 {
			t.Errorf("editor field %q name is %d chars, want at most 256", field.Name, size)
		}
		if size := len([]rune(field.Value)); size > 1024 {
			t.Errorf("editor field %q value is %d chars, want at most 1024", field.Name, size)
		}
		if field.Value == "" {
			t.Errorf("editor field %q is empty", field.Name)
		}
	}
}

func TestPanelAndEditorStayInsideDiscordLimits(t *testing.T) {
	longTitle := strings.Repeat("W", 100)
	cjkTitle := strings.Repeat("가나다라마", 24)

	cases := []struct {
		name  string
		songs []*queue.Song
	}{
		{"empty queue", nil},
		{"single song", checkSongs(1, "Solo")},
		{"two songs", checkSongs(2, "Pair")},
		{"fifty songs", checkSongs(50, "Track")},
		{"long ascii titles", []*queue.Song{checkSong(1, longTitle), checkSong(2, longTitle), checkSong(3, longTitle)}},
		{"cjk titles", []*queue.Song{checkSong(1, cjkTitle), checkSong(2, cjkTitle), checkSong(3, cjkTitle)}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			state := checkPanelState(testCase.songs, nil, true)
			rows := automix.HookHydrateTransitionRows("check-guild", state, *state.HookPairs())
			totalPages := automix.HookTransitionPageCount(*state.HookPairs())

			for page := 1; page <= totalPages; page++ {
				pageRows := automix.HookHydrateTransitionRows("check-guild", state, automix.HookTransitionPageSlice(*state.HookPairs(), page))
				components := automix.HookCreateTransitionPanelComponents("check-guild", pageRows, page, totalPages)
				rowCount, selects, buttons := inspectComponents(components)

				if rowCount > 5 {
					t.Errorf("page %d has %d action rows, want at most 5", page, rowCount)
				}
				for _, menu := range selects {
					for _, violation := range checkSelectMenu(fmt.Sprintf("page %d list", page), menu) {
						t.Error(violation)
					}
				}
				for _, button := range buttons {
					if size := len([]rune(button.CustomID)); size > automix.HookDiscordLabelLimit {
						t.Errorf("page %d button custom id is %d chars, want at most %d", page, size, automix.HookDiscordLabelLimit)
					}
				}

				embed := automix.HookCreateTransitionPanelEmbed("check-guild", state, pageRows, page, totalPages)
				if size := len([]rune(embed.Description)); size > 4096 {
					t.Errorf("page %d description is %d chars, want at most 4096", page, size)
				}
				if size := len([]rune(embed.Title)); size > 256 {
					t.Errorf("page %d title is %d chars, want at most 256", page, size)
				}
			}

			for _, row := range rows {
				for index := range *automix.HookEditorTabs {
					checkEditor(t, row, &(*automix.HookEditorTabs)[index])
				}
			}
		})
	}
}

func fourSongPanel(t *testing.T) []*automix.HookTransitionRow {
	t.Helper()

	rows := checkRowsFor(checkSongs(4, "Track"))
	if len(rows) < 2 {
		t.Fatalf("a four song queue produced %d rows, want at least 2", len(rows))
	}
	return rows
}

var checkLocation = automix.HookBuildPanelLocation(automix.HookPanelLocationFields{MessageID: "123456789012345678", Page: 2})

func TestPanelCustomIDsRouteToTheirPages(t *testing.T) {
	rows := fourSongPanel(t)
	_, selects, buttons := inspectComponents(automix.HookCreateTransitionPanelComponents("check-guild", rows, 2, 3))

	if len(selects) != 1 || selects[0].CustomID != automix.HookTransitionPickRoute+":2" {
		t.Errorf("selects = %+v, want one picker routed to page 2", selects)
	}
	want := []string{
		automix.HookTransitionPageRoute + ":1", automix.HookTransitionPageRoute + ":3",
		automix.HookTransitionPageRoute + ":2", automix.HookMixingSettingsRoute,
	}
	if len(buttons) != len(want) {
		t.Fatalf("built %d buttons, want %d", len(buttons), len(want))
	}
	for index, button := range buttons {
		if button.CustomID != want[index] {
			t.Errorf("button %d routes to %q, want %q", index, button.CustomID, want[index])
		}
	}
}

func TestEditorCustomIDsRoundTripToTheirCategory(t *testing.T) {
	rows := fourSongPanel(t)
	songArgument := strconv.Itoa((*rows[0].HookFromSong()).ID)
	pageArgument := strconv.Itoa(*checkLocation.HookPage())

	categoriesSeen := map[transition.Category]bool{}
	for index := range *automix.HookEditorTabs {
		tab := &(*automix.HookEditorTabs)[index]
		_, selects, _ := inspectComponents(automix.HookCreateTransitionEditorComponents("check-guild", rows[0], tab, checkLocation))
		for _, menu := range selects {
			parts := strings.Split(menu.CustomID, ":")
			if len(parts) != 5 || parts[0] != automix.HookTransitionStyleRoute || parts[2] != songArgument || parts[3] != *checkLocation.HookMessageID() || parts[4] != pageArgument {
				t.Errorf("custom id %q does not match %s:<category>:%s:%s:%s", menu.CustomID, automix.HookTransitionStyleRoute, songArgument, *checkLocation.HookMessageID(), pageArgument)
				continue
			}
			category, ok := transition.ParseCategory(parts[1])
			if !ok {
				t.Errorf("custom id %q yielded the unknown category %q", menu.CustomID, parts[1])
				continue
			}
			categoriesSeen[category] = true
		}
	}

	if want := len(transition.StyleCategories()) + len(transition.SettingCategories()); len(categoriesSeen) != want {
		t.Errorf("recovered %d categories across the tabs, want %d", len(categoriesSeen), want)
	}
}

func TestTabButtonsRouteToEachTab(t *testing.T) {
	rows := fourSongPanel(t)
	songArgument := strconv.Itoa((*rows[0].HookFromSong()).ID)
	_, _, buttons := inspectComponents(automix.HookCreateTransitionEditorComponents("check-guild", rows[0], editorTab("incoming"), checkLocation))

	for index, button := range buttons {
		key := (*automix.HookEditorTabs)[index].HookKey()
		want := strings.Join([]string{automix.HookTransitionTabRoute, key, songArgument, *checkLocation.HookMessageID(), "2"}, ":")
		if button.CustomID != want {
			t.Errorf("tab button %d routes to %q, want %q", index, button.CustomID, want)
		}
		if isCurrent := key == "incoming"; button.Disabled != isCurrent || (button.Style == discordgo.PrimaryButton) != isCurrent {
			t.Errorf("tab %s disabled=%t style=%d, want only the open tab highlighted and disabled", key, button.Disabled, button.Style)
		}
	}
}

func TestOutroEditorHidesTheIncomingSide(t *testing.T) {
	rows := checkRowsFor(checkSongs(1, "Solo"))
	if len(rows) != 1 || !rows[0].HookIsOutro() {
		t.Fatalf("a single song gave %d rows, want one outro row", len(rows))
	}

	_, _, buttons := inspectComponents(automix.HookCreateTransitionEditorComponents("check-guild", rows[0], editorTab("outgoing"), checkLocation))
	for index, button := range buttons {
		if key := (*automix.HookEditorTabs)[index].HookKey(); key == "incoming" && !button.Disabled {
			t.Error("the outro editor lets the incoming tab open, want it disabled")
		}
	}

	_, selects, _ := inspectComponents(automix.HookCreateTransitionEditorComponents("check-guild", rows[0], editorTab("settings"), checkLocation))
	if len(selects) != 1 || !strings.Contains(selects[0].CustomID, ":"+string(transition.CategoryLoop)+":") {
		t.Errorf("outro settings tab has %d dropdowns, want only the loop", len(selects))
	}
}
