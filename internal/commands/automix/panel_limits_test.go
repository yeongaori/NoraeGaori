package automix

import (
	"fmt"
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
	return hydrateTransitionRows("check-guild", state, state.pairs)
}

func checkRowsWithGuild(songs []*queue.Song, guildOverrides transition.StyleOverrides) []transitionRow {
	state := checkPanelState(songs, guildOverrides, true)
	return hydrateTransitionRows("check-guild", state, state.pairs)
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
			rows := hydrateTransitionRows("check-guild", state, state.pairs)
			totalPages := transitionPageCount(state.pairs)

			for page := 1; page <= totalPages; page++ {
				pageRows := hydrateTransitionRows("check-guild", state, transitionPageSlice(state.pairs, page))
				components := createTransitionPanelComponents("check-guild", pageRows, page, totalPages, newPanelToken())
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

				embed := createTransitionPanelEmbed("check-guild", state, pageRows, page, totalPages)
				if size := len([]rune(embed.Description)); size > 4096 {
					t.Errorf("page %d description is %d chars, want at most 4096", page, size)
				}
				if size := len([]rune(embed.Title)); size > 256 {
					t.Errorf("page %d title is %d chars, want at most 256", page, size)
				}
			}

			for _, row := range rows {
				components := createTransitionEditorComponents("check-guild", state, row, newPanelToken())
				rowCount, selects, _ := inspectComponents(components)

				if rowCount != len(transitionCategories) {
					t.Errorf("editor has %d action rows, want %d", rowCount, len(transitionCategories))
				}
				for _, menu := range selects {
					for _, violation := range checkSelectMenu("editor", menu) {
						t.Error(violation)
					}
				}

				embed := createTransitionEditorEmbed("check-guild", state, row, "")
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
				break
			}
		})
	}
}

func fourSongPanel(t *testing.T) (panelState, []transitionRow) {
	t.Helper()

	state := checkPanelState(checkSongs(4, "Track"), transition.StyleOverrides{}, true)
	rows := hydrateTransitionRows("check-guild", state, state.pairs)
	if len(rows) < 2 {
		t.Fatalf("a four song queue produced %d rows, want at least 2", len(rows))
	}
	return state, rows
}

func TestPanelCustomIDsAreUnique(t *testing.T) {
	state, rows := fourSongPanel(t)
	token := newPanelToken()
	components := createTransitionPanelComponents("check-guild", rows, 1, transitionPageCount(state.pairs), token)
	_, selects, buttons := inspectComponents(components)

	seen := map[string]bool{}
	for _, menu := range selects {
		if seen[menu.CustomID] {
			t.Errorf("duplicate select custom id %q", menu.CustomID)
		}
		seen[menu.CustomID] = true
	}
	for _, button := range buttons {
		if seen[button.CustomID] {
			t.Errorf("duplicate button custom id %q", button.CustomID)
		}
		seen[button.CustomID] = true
	}
}

func TestPanelCustomIDsCarryThePanelToken(t *testing.T) {
	state, rows := fourSongPanel(t)
	token := newPanelToken()
	components := createTransitionPanelComponents("check-guild", rows, 1, transitionPageCount(state.pairs), token)
	_, selects, buttons := inspectComponents(components)

	for _, menu := range selects {
		if !strings.HasSuffix(menu.CustomID, token) {
			t.Errorf("select custom id %q does not end with token %q", menu.CustomID, token)
		}
	}
	for _, button := range buttons {
		if !strings.HasSuffix(button.CustomID, token) {
			t.Errorf("button custom id %q does not end with token %q", button.CustomID, token)
		}
	}
}

func TestTwoEditorsProduceDisjointCustomIDs(t *testing.T) {
	state, rows := fourSongPanel(t)

	firstToken := newPanelToken()
	secondToken := firstToken + "b"
	_, firstSelects, _ := inspectComponents(createTransitionEditorComponents("check-guild", state, rows[0], firstToken))
	_, secondSelects, _ := inspectComponents(createTransitionEditorComponents("check-guild", state, rows[1], secondToken))

	firstIDs := map[string]bool{}
	for _, menu := range firstSelects {
		firstIDs[menu.CustomID] = true
	}
	for _, menu := range secondSelects {
		if firstIDs[menu.CustomID] {
			t.Errorf("custom id %q appears in both editors", menu.CustomID)
		}
	}
}

func TestEditorCustomIDsRoundTripToTheirCategory(t *testing.T) {
	state, rows := fourSongPanel(t)

	token := newPanelToken()
	_, selects, _ := inspectComponents(createTransitionEditorComponents("check-guild", state, rows[0], token))
	suffix := fmt.Sprintf("_%s_%d", token, rows[0].fromSong.ID)

	categoriesSeen := map[string]bool{}
	for _, menu := range selects {
		if !strings.HasPrefix(menu.CustomID, "automix_style_") || !strings.HasSuffix(menu.CustomID, suffix) {
			t.Errorf("custom id %q does not match automix_style_<category>%s", menu.CustomID, suffix)
			continue
		}

		category := strings.TrimSuffix(strings.TrimPrefix(menu.CustomID, "automix_style_"), suffix)
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
