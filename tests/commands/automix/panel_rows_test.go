package automix_test

import (
	"fmt"
	"strings"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/commands/automix"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func effectiveStyle(row *automix.HookTransitionRow, category transition.Category) string {
	return transition.StyleOf(&row.HookResolved().Recipe, category)
}

func TestDescribeTrackCoversEveryAnalysisState(t *testing.T) {
	panel := &messages.T("check-guild").AutoMixPanel

	for _, check := range []struct {
		name      string
		track     *analysis.TrackAnalysis
		analyzing bool
		want      string
	}{
		{"missing while analyzing", nil, true, panel.Analyzing},
		{"missing", nil, false, panel.Unknown},
		{"no tempo while analyzing", &analysis.TrackAnalysis{}, true, panel.Analyzing},
		{"no tempo", &analysis.TrackAnalysis{}, false, panel.Unknown},
		{"no confident key", &analysis.TrackAnalysis{BPM: 128}, false, fmt.Sprintf("%.1f BPM · %s", 128.0, panel.Unknown)},
		{
			"a confident key",
			&analysis.TrackAnalysis{BPM: 128, KeyConfidence: 1, Tonic: 9, Minor: true},
			false,
			fmt.Sprintf("%.1f BPM · %s (%s)", 128.0, analysis.KeyName(9, true), analysis.CamelotCode(9, true)),
		},
	} {
		if got := automix.HookDescribeTrack("check-guild", check.track, check.analyzing); got != check.want {
			t.Errorf("%s: describeTrack = %q, want %q", check.name, got, check.want)
		}
	}
}

func TestSourceLabelsNameEveryOverrideSource(t *testing.T) {
	panel := &messages.T("check-guild").AutoMixPanel

	for source, want := range map[string]string{"guild": panel.SourceGuild, "song": panel.SourceSong, "auto": panel.SourceAuto} {
		if got := automix.HookSourceLabel("check-guild", source); got != want {
			t.Errorf("sourceLabel(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestFindTransitionPairLooksUpTheOutgoingSong(t *testing.T) {
	pairs := automix.HookTransitionPairs(checkSongs(3, "Track"))

	if pair := automix.HookFindTransitionPair(pairs, 2); pair == nil || (*pair.HookFromSong()).ID != 2 {
		t.Errorf("findTransitionPair(2) = %+v, want the pair leaving song 2", pair)
	}
	if pair := automix.HookFindTransitionPair(pairs, 99); pair != nil {
		t.Error("a missing song was found")
	}
	if pair := automix.HookFindTransitionPair(pairs, 2); pair != &pairs[1] {
		t.Error("the found pair is a copy, want the pair stored in the panel state")
	}
}

func TestEmptyQueueYieldsNoTransitions(t *testing.T) {
	if rows := checkRowsFor(nil); len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestSingleSongYieldsOnlyAnOutro(t *testing.T) {
	rows := checkRowsFor(checkSongs(1, "Solo"))

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !rows[0].HookIsOutro() {
		t.Error("the only row is not an outro")
	}
}

func TestTwoSongsYieldOneTransitionAndAnOutro(t *testing.T) {
	rows := checkRowsFor(checkSongs(2, "Pair"))

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].HookIsOutro() {
		t.Error("the first row is an outro, want a transition")
	}
	if !rows[1].HookIsOutro() {
		t.Error("the second row is not an outro")
	}
}

func TestFiftySongsYieldFortyNineTransitionsPlusAnOutro(t *testing.T) {
	state := checkPanelState(checkSongs(50, "Track"), nil, true)
	rows := automix.HookHydrateTransitionRows("check-guild", state, *state.HookPairs())

	if len(rows) != 50 {
		t.Errorf("got %d rows, want 50", len(rows))
	}
	if pages := automix.HookTransitionPageCount(*state.HookPairs()); pages != 10 {
		t.Errorf("got %d pages, want 10", pages)
	}
}

func TestLiveSongsDropBothAdjacentTransitions(t *testing.T) {
	songs := checkSongs(4, "Track")
	songs[1].IsLive = true
	rows := checkRowsFor(songs)

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	outros := 0
	for i, row := range rows {
		if row.HookIsOutro() {
			outros++
			if (*row.HookFromSong()).IsLive {
				t.Errorf("row %d is an outro for a live song", i)
			}
			continue
		}
		if (*row.HookFromSong()).IsLive || (*row.HookToSong()).IsLive {
			t.Errorf("row %d still touches a live song", i)
		}
	}
	if outros != 1 {
		t.Errorf("got %d outros, want 1", outros)
	}
}

func TestALiveLastSongGetsNoOutro(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[2].IsLive = true
	rows := checkRowsFor(songs)

	if len(rows) != 1 || *rows[0].HookFromIndex() != 0 || *rows[0].HookToIndex() != 1 {
		t.Errorf("rows = %+v, want only the 1 to 2 transition for a live last song", rows)
	}
}

func TestSongOverrideMarksOnlyItsOwnTransition(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixOverrides = transition.CategoryFXOut.Override("echo_half_cut_end")
	rows := checkRowsFor(songs)

	if len(rows) < 2 {
		t.Fatalf("got %d rows, want at least 2", len(rows))
	}
	if got := effectiveStyle(rows[0], transition.CategoryFXOut); got != "echo_half_cut_end" {
		t.Errorf("first effect = %q, want echo_half_cut_end", got)
	}
	if got := rows[0].HookSource(transition.CategoryFXOut); got != "song" {
		t.Errorf("first effect source = %q, want song", got)
	}
	if got := rows[1].HookSource(transition.CategoryFXOut); got != "auto" {
		t.Errorf("second effect source = %q, want auto", got)
	}
}

func TestGuildDefaultsApplyWhereTheSongDoesNotOverride(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixOverrides = transition.CategoryFilterOut.Override("low_pass")
	rows := checkRowsWithGuild(songs, map[string]string{"filter_out": "high_pass", "fx_out": "reverb_out_end"})

	if len(rows) < 2 {
		t.Fatalf("got %d rows, want at least 2", len(rows))
	}
	for _, want := range []struct {
		row      int
		category transition.Category
		style    string
		source   string
	}{
		{0, transition.CategoryFilterOut, "low_pass", "song"},
		{0, transition.CategoryFXOut, "reverb_out_end", "guild"},
		{1, transition.CategoryFilterOut, "high_pass", "guild"},
	} {
		if got := effectiveStyle(rows[want.row], want.category); got != want.style {
			t.Errorf("row %d %s = %q, want %q", want.row, want.category, got, want.style)
		}
		if got := rows[want.row].HookSource(want.category); got != want.source {
			t.Errorf("row %d %s source = %q, want %s", want.row, want.category, got, want.source)
		}
	}
}

func TestInvalidStoredChoicesShowAsAuto(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixOverrides = map[string]string{"fx_out": "bogus", "preset": "7", "length": "four_bars"}
	rows := checkRowsFor(songs)

	if got := rows[0].HookSource(transition.CategoryFXOut); got != "auto" {
		t.Errorf("an invalid stored effect has source %q, want auto", got)
	}
	if got := rows[0].HookSource(transition.CategoryPreset); got != "auto" {
		t.Errorf("an invalid stored preset has source %q, want auto", got)
	}
	if got := rows[0].HookSource(transition.CategoryLength); got != "song" {
		t.Errorf("a valid stored length has source %q, want song", got)
	}
}

func TestPanelHidesAutoSelectionWhenAutoMixIsOff(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixOverrides = transition.CategoryFXOut.Override("reverb_out_end")

	state := checkPanelState(songs, transition.ExpandLegacy(transition.ShortcutEQ, "quick_bass"), false)
	rows := automix.HookHydrateTransitionRows("check-guild", state, *state.HookPairs())
	if len(rows) == 0 {
		t.Fatal("no rows built")
	}

	for _, want := range []struct {
		category transition.Category
		style    string
		source   string
	}{
		{transition.CategoryVolumeOut, "cross_shape", "auto"},
		{transition.CategoryFilterIn, "none", "auto"},
		{transition.CategoryFXOut, "reverb_out_end", "song"},
		{transition.CategoryEQOut, "bass_fast_one_bar_from_end", "guild"},
		{transition.CategoryEQIn, "bass_fast_at_end", "guild"},
	} {
		if got := effectiveStyle(rows[0], want.category); got != want.style {
			t.Errorf("%s = %q, want %q", want.category, got, want.style)
		}
		if got := rows[0].HookSource(want.category); got != want.source {
			t.Errorf("%s source = %q, want %q", want.category, got, want.source)
		}
	}
	if rows[0].HookOverlap() != nil {
		t.Error("the panel previewed an overlap with AutoMix off")
	}
}

func TestPanelDropsALoopThePlayerCannotRun(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixOverrides = transition.CategoryLoop.Override("eight_beats")
	rows := checkRowsFor(songs)

	if len(rows) == 0 {
		t.Fatal("no rows built")
	}
	if got := effectiveStyle(rows[0], transition.CategoryLoop); got != "none" {
		t.Errorf("loop = %q, want none", got)
	}
	if got := rows[0].HookSource(transition.CategoryLoop); got != "song" {
		t.Errorf("loop source = %q, want song", got)
	}
}

func analyzingRows(songs []*queue.Song, pending map[int]int) []*automix.HookTransitionRow {
	state := checkPanelState(songs, nil, true)
	*state.HookPending() = pending
	return automix.HookHydrateTransitionRows("check-guild", state, *state.HookPairs())
}

func TestAnalyzingFollowsThePendingSongs(t *testing.T) {
	songs := checkSongs(3, "Track")

	idleRows := analyzingRows(songs, nil)
	if len(idleRows) != 3 {
		t.Fatalf("got %d rows, want 3", len(idleRows))
	}
	for index, row := range idleRows {
		if *row.HookFromAnalyzing() || *row.HookToAnalyzing() {
			t.Errorf("row %d reports analyzing with nothing pending", index)
		}
	}

	playingRows := analyzingRows(songs, map[int]int{songs[0].ID: 1})
	if !*playingRows[0].HookFromAnalyzing() {
		t.Error("the playing song is not flagged while its ending is pending")
	}
	if *playingRows[0].HookToAnalyzing() || *playingRows[1].HookFromAnalyzing() {
		t.Error("a song that is not pending is flagged as analyzing")
	}

	laterRows := analyzingRows(songs, map[int]int{songs[2].ID: 2})
	if *laterRows[0].HookFromAnalyzing() || *laterRows[0].HookToAnalyzing() {
		t.Error("the first transition is flagged although only the last song is pending")
	}
	if !*laterRows[1].HookToAnalyzing() || !*laterRows[2].HookFromAnalyzing() {
		t.Error("the pending last song is not flagged in both rows it appears in")
	}
	if !laterRows[2].HookIsOutro() {
		t.Fatal("the last row is not an outro")
	}
	if *laterRows[2].HookToAnalyzing() {
		t.Error("the outro flags a nonexistent next track as analyzing")
	}
}

func TestOutroRowResolvesToTheAutoOutroRecipe(t *testing.T) {
	rows := checkRowsFor(checkSongs(2, "Track"))
	outro := rows[len(rows)-1]

	if outro.HookResolved().Recipe != *transition.OutroRecipe() {
		t.Errorf("outro recipe = %s, want the auto outro %s", &outro.HookResolved().Recipe, transition.OutroRecipe())
	}
	want := messages.T("check-guild").AutoMixPanel.EndsNaturally
	if got := automix.HookDescribeRecipe("check-guild", outro, true); got != want {
		t.Errorf("outro row reads %q, want %q", got, want)
	}
}

func TestPreviewShowsTheFadeWhenTheSongsCanHoldIt(t *testing.T) {
	songs := checkSongs(2, "Track")
	for _, song := range songs {
		song.Duration = "3:00"
	}
	row := checkRowsFor(songs)[0]
	if got := effectiveStyle(row, transition.CategoryEQOut); got != "bass_fast" {
		t.Errorf("eq out = %q, want the Fade preset's bass_fast for two 3-minute songs", got)
	}
	panel := &messages.T("check-guild").AutoMixPanel
	text := automix.HookDescribeRecipe("check-guild", row, true)
	for _, part := range []string{panel.StyleLabels["preset.1"], fmt.Sprintf(panel.SecondsFormat, 5.0), panel.NotBeatmatched} {
		if !strings.Contains(text, part) {
			t.Errorf("row reads %q, want it to mention %q", text, part)
		}
	}

	if got := effectiveStyle(checkRowsFor(checkSongs(2, "Track"))[0], transition.CategoryEQOut); got != "none" {
		t.Errorf("eq out = %q, want the plain crossfade when no overlap fits songs of unknown length", got)
	}
}

func TestSongSettingsShapeThePreview(t *testing.T) {
	songs := checkSongs(2, "Track")
	for _, song := range songs {
		song.Duration = "3:00"
	}
	songs[0].AutoMixOverrides = map[string]string{"preset": "11", "length": "two_bars"}
	row := checkRowsFor(songs)[0]

	overlap := row.HookOverlap()
	if overlap == nil || overlap.Preset != 11 || overlap.Bars != 2 || overlap.Length != 4 {
		t.Fatalf("preview = %v, want preset 11 over two bars of 2s", overlap)
	}
	if got := effectiveStyle(row, transition.CategoryVolumeOut); got != "fast" {
		t.Errorf("volume out = %q, want the chosen simple cut", got)
	}
	text := automix.HookDescribeRecipe("check-guild", row, true)
	panel := &messages.T("check-guild").AutoMixPanel
	for _, part := range []string{panel.StyleLabels["preset.11"], fmt.Sprintf(panel.BarsFormat, 2), panel.CategoryLabels["length"], panel.OverrideMarker} {
		if !strings.Contains(text, part) {
			t.Errorf("row reads %q, want it to mention %q", text, part)
		}
	}
}

func TestOutroHonoursTheLastSongsOverride(t *testing.T) {
	songs := checkSongs(2, "Track")
	songs[1].AutoMixOverrides = transition.CategoryFXOut.Override("reverb_out_center")
	rows := checkRowsFor(songs)
	last := rows[len(rows)-1]

	if !last.HookIsOutro() {
		t.Fatal("the last row is not an outro")
	}
	if got := effectiveStyle(last, transition.CategoryFXOut); got != "reverb_out_center" {
		t.Errorf("effect = %q, want reverb_out_center", got)
	}
	if got := last.HookSource(transition.CategoryFXOut); got != "song" {
		t.Errorf("effect source = %q, want song", got)
	}
}
