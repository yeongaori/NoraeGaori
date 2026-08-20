package automix

import (
	"fmt"
	"noraegaori/internal/audio/transition"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/queue"
)

type panelCheckCollector struct{ t *testing.T }

func (c *panelCheckCollector) add(name string, passed bool, format string, args ...interface{}) {
	c.t.Helper()
	c.t.Run(name, func(t *testing.T) {
		t.Helper()
		if !passed {
			t.Errorf(format, args...)
		}
	})
}

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

func checkPanelLimits(c *panelCheckCollector) {
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

	violations := []string{}
	for _, testCase := range cases {
		state := checkPanelState(testCase.songs, transition.StyleOverrides{}, true)
		rows := hydrateTransitionRows("check-guild", state, state.pairs)
		totalPages := transitionPageCount(state.pairs)

		for page := 1; page <= totalPages; page++ {
			pageRows := hydrateTransitionRows("check-guild", state, transitionPageSlice(state.pairs, page))
			components := createTransitionPanelComponents("check-guild", pageRows, page, totalPages, newPanelToken())
			rowCount, selects, buttons := inspectComponents(components)

			if rowCount > 5 {
				violations = append(violations, fmt.Sprintf("%s page %d: %d action rows", testCase.name, page, rowCount))
			}
			for _, menu := range selects {
				violations = append(violations, checkSelectMenu(fmt.Sprintf("%s page %d list", testCase.name, page), menu)...)
			}
			for _, button := range buttons {
				if len([]rune(button.CustomID)) > discordLabelLimit {
					violations = append(violations, fmt.Sprintf("%s page %d: button custom id %d chars", testCase.name, page, len(button.CustomID)))
				}
			}

			embed := createTransitionPanelEmbed("check-guild", state, pageRows, page, totalPages)
			if len([]rune(embed.Description)) > 4096 {
				violations = append(violations, fmt.Sprintf("%s page %d: description %d chars", testCase.name, page, len([]rune(embed.Description))))
			}
			if len([]rune(embed.Title)) > 256 {
				violations = append(violations, fmt.Sprintf("%s page %d: title %d chars", testCase.name, page, len([]rune(embed.Title))))
			}
		}

		for _, row := range rows {
			components := createTransitionEditorComponents("check-guild", state, row, newPanelToken())
			rowCount, selects, _ := inspectComponents(components)
			if rowCount != len(transitionCategories) {
				violations = append(violations, fmt.Sprintf("%s editor: %d action rows", testCase.name, rowCount))
			}
			for _, menu := range selects {
				violations = append(violations, checkSelectMenu(testCase.name+" editor", menu)...)
			}

			embed := createTransitionEditorEmbed("check-guild", state, row, "")
			if len([]rune(embed.Title)) > 256 {
				violations = append(violations, fmt.Sprintf("%s editor: title %d chars", testCase.name, len([]rune(embed.Title))))
			}
			for _, field := range embed.Fields {
				if len([]rune(field.Name)) > 256 || len([]rune(field.Value)) > 1024 {
					violations = append(violations, fmt.Sprintf("%s editor: field %q oversized", testCase.name, field.Name))
				}
				if field.Value == "" {
					violations = append(violations, fmt.Sprintf("%s editor: field %q empty", testCase.name, field.Name))
				}
			}
			break
		}
	}

	c.add("panel and editor stay inside Discord limits", len(violations) == 0,
		"%d violations%s", len(violations), formatViolations(violations))
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

func formatViolations(violations []string) string {
	if len(violations) == 0 {
		return ""
	}
	limit := len(violations)
	if limit > 3 {
		limit = 3
	}
	return ": " + strings.Join(violations[:limit], "; ")
}

func checkPanelIdentifiers(c *panelCheckCollector) {
	state := checkPanelState(checkSongs(4, "Track"), transition.StyleOverrides{}, true)
	rows := hydrateTransitionRows("check-guild", state, state.pairs)
	if len(rows) < 2 {
		c.add("panel builds transitions for a four song queue", false, "got %d rows", len(rows))
		return
	}

	token := newPanelToken()
	components := createTransitionPanelComponents("check-guild", rows, 1, transitionPageCount(state.pairs), token)
	_, selects, buttons := inspectComponents(components)

	seen := map[string]bool{}
	duplicates := 0
	for _, menu := range selects {
		if seen[menu.CustomID] {
			duplicates++
		}
		seen[menu.CustomID] = true
	}
	for _, button := range buttons {
		if seen[button.CustomID] {
			duplicates++
		}
		seen[button.CustomID] = true
	}
	c.add("panel custom ids are unique", duplicates == 0, "%d components, %d duplicates", len(seen), duplicates)

	tokenSuffixed := true
	for id := range seen {
		if !strings.HasSuffix(id, token) {
			tokenSuffixed = false
		}
	}
	c.add("panel custom ids carry the panel token", tokenSuffixed, "token=%s", token)

	firstToken := newPanelToken()
	secondToken := firstToken + "b"
	firstEditor := createTransitionEditorComponents("check-guild", state, rows[0], firstToken)
	secondEditor := createTransitionEditorComponents("check-guild", state, rows[1], secondToken)

	_, firstSelects, _ := inspectComponents(firstEditor)
	_, secondSelects, _ := inspectComponents(secondEditor)

	firstIDs := map[string]bool{}
	for _, menu := range firstSelects {
		firstIDs[menu.CustomID] = true
	}
	overlap := 0
	for _, menu := range secondSelects {
		if firstIDs[menu.CustomID] {
			overlap++
		}
	}
	c.add("two editors produce disjoint custom ids", overlap == 0,
		"%d first, %d second, %d overlap", len(firstSelects), len(secondSelects), overlap)

	categoriesSeen := map[string]bool{}
	parseFailures := 0
	suffix := fmt.Sprintf("_%s_%d", firstToken, rows[0].fromSong.ID)
	for _, menu := range firstSelects {
		if !strings.HasPrefix(menu.CustomID, "automix_style_") || !strings.HasSuffix(menu.CustomID, suffix) {
			parseFailures++
			continue
		}
		category := strings.TrimSuffix(strings.TrimPrefix(menu.CustomID, "automix_style_"), suffix)
		if !transition.ValidStyle(category, queue.AutoMixStyleAuto) || transition.StyleValues(category) == nil {
			parseFailures++
			continue
		}
		categoriesSeen[category] = true
	}
	c.add("editor custom ids round-trip to their category",
		parseFailures == 0 && len(categoriesSeen) == len(transitionCategories),
		"%d categories, %d parse failures", len(categoriesSeen), parseFailures)
}

func checkPanelRows(c *panelCheckCollector) {
	rows := checkRowsFor(nil)
	c.add("empty queue yields no transitions", len(rows) == 0, "%d rows", len(rows))

	rows = checkRowsFor(checkSongs(1, "Solo"))
	c.add("single song yields only an outro", len(rows) == 1 && rows[0].isOutro(), "%d rows", len(rows))

	rows = checkRowsFor(checkSongs(2, "Pair"))
	c.add("two songs yield one transition and an outro",
		len(rows) == 2 && !rows[0].isOutro() && rows[1].isOutro(), "%d rows", len(rows))

	fiftyState := checkPanelState(checkSongs(50, "Track"), transition.StyleOverrides{}, true)
	rows = hydrateTransitionRows("check-guild", fiftyState, fiftyState.pairs)
	pages := transitionPageCount(fiftyState.pairs)
	c.add("fifty songs yield forty-nine transitions plus an outro", len(rows) == 50 && pages == 10,
		"%d rows across %d pages", len(rows), pages)

	songs := checkSongs(4, "Track")
	songs[1].IsLive = true
	rows = checkRowsFor(songs)
	dropped := true
	outros := 0
	for _, row := range rows {
		if row.isOutro() {
			outros++
			if row.fromSong.IsLive {
				dropped = false
			}
			continue
		}
		if row.fromSong.IsLive || row.toSong.IsLive {
			dropped = false
		}
	}
	c.add("live songs drop both adjacent transitions", len(rows) == 2 && outros == 1 && dropped,
		"%d rows remain, %d outros", len(rows), outros)

	songs = checkSongs(3, "Track")
	songs[2].IsLive = true
	rows = checkRowsFor(songs)
	liveOutros := 0
	for _, row := range rows {
		if row.isOutro() {
			liveOutros++
		}
	}
	c.add("a live last song gets no outro", liveOutros == 0, "%d rows, %d outros", len(rows), liveOutros)

	songs = checkSongs(3, "Track")
	songs[0].AutoMixStyleEffect = "echo_half_cut_end"
	rows = checkRowsFor(songs)
	marked := len(rows) > 0 && rows[0].effective["effect"] == "echo_half_cut_end" && rows[0].source["effect"] == "song"
	autoElsewhere := len(rows) > 1 && rows[1].source["effect"] == "auto"
	c.add("song override marks only its own transition", marked && autoElsewhere,
		"first=%s/%s second=%s", rows[0].effective["effect"], rows[0].source["effect"], rows[1].source["effect"])

	songs = checkSongs(3, "Track")
	songs[0].AutoMixStyleFilter = "lowpass_out"
	rows = checkRowsWithGuild(songs, transition.StyleOverrides{Filter: "lowpass_in", Effect: "reverb_out_end"})
	songWins := len(rows) > 0 && rows[0].effective["filter"] == "lowpass_out" && rows[0].source["filter"] == "song"
	guildWins := len(rows) > 0 && rows[0].effective["effect"] == "reverb_out_end" && rows[0].source["effect"] == "guild"
	guildAppliesEverywhere := len(rows) > 1 && rows[1].effective["filter"] == "lowpass_in" && rows[1].source["filter"] == "guild"
	c.add("guild defaults apply where the song does not override",
		songWins && guildWins && guildAppliesEverywhere,
		"first filter=%s/%s effect=%s/%s second filter=%s/%s",
		rows[0].effective["filter"], rows[0].source["filter"],
		rows[0].effective["effect"], rows[0].source["effect"],
		rows[1].effective["filter"], rows[1].source["filter"])

	unanalyzed := describeTrack("check-guild", nil, true)
	unavailable := describeTrack("check-guild", nil, false)
	c.add("missing analysis renders a placeholder", unanalyzed != "" && unavailable != "" && unanalyzed != unavailable,
		"analyzing=%q idle=%q", unanalyzed, unavailable)

	songs = checkSongs(3, "Track")
	songs[0].AutoMixStyleEffect = "reverb_out_end"
	offState := checkPanelState(songs, transition.StyleOverrides{EQ: "quick_bass"}, false)
	offRows := hydrateTransitionRows("check-guild", offState, offState.pairs)
	offDefaults := len(offRows) > 0 && offRows[0].effective["volume"] == "smooth" &&
		offRows[0].effective["filter"] == "none" && offRows[0].source["volume"] == "auto"
	offOverrides := len(offRows) > 0 && offRows[0].effective["effect"] == "reverb_out_end" &&
		offRows[0].source["effect"] == "song" && offRows[0].effective["eq"] == "quick_bass" &&
		offRows[0].source["eq"] == "guild"
	c.add("panel hides auto selection when automix is off", offDefaults && offOverrides,
		"volume=%s/%s eq=%s/%s effect=%s/%s",
		offRows[0].effective["volume"], offRows[0].source["volume"],
		offRows[0].effective["eq"], offRows[0].source["eq"],
		offRows[0].effective["effect"], offRows[0].source["effect"])

	songs = checkSongs(3, "Track")
	songs[0].AutoMixStyleLoop = "eight_beats"
	loopRows := checkRowsFor(songs)
	loopDropped := len(loopRows) > 0 && loopRows[0].effective["loop"] == "none" && loopRows[0].source["loop"] == "song"
	c.add("panel drops a loop the player cannot run", loopDropped,
		"loop=%s/%s", loopRows[0].effective["loop"], loopRows[0].source["loop"])

	songs = checkSongs(2, "Track")
	idleState := checkPanelState(songs, transition.StyleOverrides{}, true)
	idleState.backfillActive = false
	idleRows := hydrateTransitionRows("check-guild", idleState, idleState.pairs)
	busyRows := checkRowsFor(songs)
	perTrack := len(idleRows) == 2 && len(busyRows) == 2 &&
		!idleRows[0].fromAnalyzing && !idleRows[0].toAnalyzing &&
		busyRows[0].fromAnalyzing && busyRows[0].toAnalyzing &&
		busyRows[1].isOutro() && busyRows[1].fromAnalyzing && !busyRows[1].toAnalyzing
	c.add("analyzing state follows the backfill worker", perTrack,
		"idle=%v busy=%v", idleRows[0].fromAnalyzing, busyRows[0].fromAnalyzing)

	outroRows := checkRowsFor(checkSongs(2, "Track"))
	outroRow := outroRows[len(outroRows)-1]
	autoOutro := transition.AutoOutroStyles(outroRow.fromAnalysis)
	outroValid := true
	for _, category := range transitionCategories {
		if !transition.ValidStyle(category, autoOutro[category]) {
			outroValid = false
		}
		if outroRow.effective[category] != autoOutro[category] {
			outroValid = false
		}
	}
	c.add("outro row resolves to the auto outro recipe", outroValid, "effective=%v auto=%v",
		outroRow.effective, autoOutro)

	songs = checkSongs(2, "Track")
	songs[1].AutoMixStyleEffect = "reverb_out_center"
	overriddenOutro := checkRowsFor(songs)
	last := overriddenOutro[len(overriddenOutro)-1]
	outroOverride := last.isOutro() && last.effective["effect"] == "reverb_out_center" && last.source["effect"] == "song"
	c.add("outro honours the last song's override", outroOverride,
		"effect=%s/%s", last.effective["effect"], last.source["effect"])
}

func TestCheckPanelRows(t *testing.T)        { checkPanelRows(&panelCheckCollector{t: t}) }
func TestCheckPanelLimits(t *testing.T)      { checkPanelLimits(&panelCheckCollector{t: t}) }
func TestCheckPanelIdentifiers(t *testing.T) { checkPanelIdentifiers(&panelCheckCollector{t: t}) }
