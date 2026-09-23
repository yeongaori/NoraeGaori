package automix

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/queue"
)

func checkSong(id int, title string) *queue.Song {
	return &queue.Song{
		ID:                 id,
		Title:              title,
		URL:                fmt.Sprintf("https://example.invalid/watch?v=%d", id),
		AutoMixStyleVolume: queue.AutoMixStyleAuto,
		AutoMixStyleEQ:     queue.AutoMixStyleAuto,
		AutoMixStyleFilter: queue.AutoMixStyleAuto,
		AutoMixStyleEffect: queue.AutoMixStyleAuto,
		AutoMixStyleLoop:   queue.AutoMixStyleAuto,
	}
}

func checkSongs(count int, title string) []*queue.Song {
	songs := make([]*queue.Song, 0, count)
	for i := 0; i < count; i++ {
		songs = append(songs, checkSong(i+1, fmt.Sprintf("%s %d", title, i+1)))
	}
	return songs
}

func checkPanelState(songs []*queue.Song, guildOverrides transition.StyleOverrides, autoSelect bool) panelState {
	return panelState{
		pairs:          transitionPairs(songs),
		guildOverrides: guildOverrides,
		autoSelect:     autoSelect,
		crossfade:      true,
		autoMixBeats:   16,
		crossfadeSec:   8,
		backfillActive: true,
	}
}

func checkRowsFor(songs []*queue.Song) []transitionRow {
	state := checkPanelState(songs, transition.StyleOverrides{}, true)
	return hydrateTransitionRows("check-guild", &state, state.pairs)
}

func checkRowsWithGuild(songs []*queue.Song, guildOverrides transition.StyleOverrides) []transitionRow {
	state := checkPanelState(songs, guildOverrides, true)
	return hydrateTransitionRows("check-guild", &state, state.pairs)
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

	if len([]rune(menu.CustomID)) > discordLabelLimit {
		violations = append(violations, fmt.Sprintf("%s: custom id %d chars", context, len([]rune(menu.CustomID))))
	}
	if len(menu.Options) == 0 {
		violations = append(violations, fmt.Sprintf("%s: no options", context))
	}
	if len(menu.Options) > discordSelectLimit {
		violations = append(violations, fmt.Sprintf("%s: %d options", context, len(menu.Options)))
	}
	if len([]rune(menu.Placeholder)) > 150 {
		violations = append(violations, fmt.Sprintf("%s: placeholder %d chars", context, len([]rune(menu.Placeholder))))
	}

	seen := map[string]bool{}
	defaults := 0
	for _, option := range menu.Options {
		if label := []rune(option.Label); len(label) > discordLabelLimit || len(label) == 0 {
			violations = append(violations, fmt.Sprintf("%s: label %d chars", context, len(label)))
		}
		if len([]rune(option.Description)) > discordLabelLimit {
			violations = append(violations, fmt.Sprintf("%s: description %d chars", context, len([]rune(option.Description))))
		}
		if value := []rune(option.Value); len(value) > discordLabelLimit || len(value) == 0 {
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
			state := checkPanelState(testCase.songs, transition.StyleOverrides{}, true)
			rows := hydrateTransitionRows("check-guild", &state, state.pairs)
			totalPages := transitionPageCount(state.pairs)

			for page := 1; page <= totalPages; page++ {
				pageRows := hydrateTransitionRows("check-guild", &state, transitionPageSlice(state.pairs, page))
				components := createTransitionPanelComponents("check-guild", pageRows, page, totalPages)
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
					if size := len([]rune(button.CustomID)); size > discordLabelLimit {
						t.Errorf("page %d button custom id is %d chars, want at most %d", page, size, discordLabelLimit)
					}
				}

				embed := createTransitionPanelEmbed("check-guild", &state, pageRows, page, totalPages)
				if size := len([]rune(embed.Description)); size > 4096 {
					t.Errorf("page %d description is %d chars, want at most 4096", page, size)
				}
				if size := len([]rune(embed.Title)); size > 256 {
					t.Errorf("page %d title is %d chars, want at most 256", page, size)
				}
			}

			for _, row := range rows {
				components := createTransitionEditorComponents("check-guild", &state, row, checkLocation)
				rowCount, selects, _ := inspectComponents(components)

				if rowCount != len(transitionCategories) {
					t.Errorf("editor has %d action rows, want %d", rowCount, len(transitionCategories))
				}
				for _, menu := range selects {
					for _, violation := range checkSelectMenu("editor", menu) {
						t.Error(violation)
					}
				}

				embed := createTransitionEditorEmbed("check-guild", &state, row, "")
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
		})
	}
}

func fourSongPanel(t *testing.T) (panelState, []transitionRow) {
	t.Helper()

	state := checkPanelState(checkSongs(4, "Track"), transition.StyleOverrides{}, true)
	rows := hydrateTransitionRows("check-guild", &state, state.pairs)
	if len(rows) < 2 {
		t.Fatalf("a four song queue produced %d rows, want at least 2", len(rows))
	}
	return state, rows
}

var checkLocation = &panelLocation{messageID: "123456789012345678", page: 2}

func TestPanelCustomIDsRouteToTheirPages(t *testing.T) {
	_, rows := fourSongPanel(t)
	_, selects, buttons := inspectComponents(createTransitionPanelComponents("check-guild", rows, 2, 3))

	if len(selects) != 1 || selects[0].CustomID != transitionPickRoute+":2" {
		t.Errorf("selects = %+v, want one picker routed to page 2", selects)
	}
	want := []string{transitionPageRoute + ":1", transitionPageRoute + ":3", transitionPageRoute + ":2"}
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
	state, rows := fourSongPanel(t)

	_, selects, _ := inspectComponents(createTransitionEditorComponents("check-guild", &state, rows[0], checkLocation))
	songArgument := strconv.Itoa(rows[0].fromSong.ID)
	pageArgument := strconv.Itoa(checkLocation.page)

	categoriesSeen := map[string]bool{}
	for _, menu := range selects {
		parts := strings.Split(menu.CustomID, ":")
		if len(parts) != 5 || parts[0] != transitionStyleRoute || parts[2] != songArgument || parts[3] != checkLocation.messageID || parts[4] != pageArgument {
			t.Errorf("custom id %q does not match %s:<category>:%s:%s:%s", menu.CustomID, transitionStyleRoute, songArgument, checkLocation.messageID, pageArgument)
			continue
		}

		category := parts[1]
		if !transition.ValidStyle(category, queue.AutoMixStyleAuto) {
			t.Errorf("custom id %q yielded invalid category %q", menu.CustomID, category)
			continue
		}
		if transition.StyleValues(category) == nil {
			t.Errorf("category %q has no style values", category)
			continue
		}
		categoriesSeen[category] = true
	}

	if len(categoriesSeen) != len(transitionCategories) {
		t.Errorf("recovered %d categories, want %d", len(categoriesSeen), len(transitionCategories))
	}
}
