package automix

import (
	"testing"

	"noraegaori/internal/audio/transition"
)

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
	if !rows[0].isOutro() {
		t.Error("the only row is not an outro")
	}
}

func TestTwoSongsYieldOneTransitionAndAnOutro(t *testing.T) {
	rows := checkRowsFor(checkSongs(2, "Pair"))

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].isOutro() {
		t.Error("the first row is an outro, want a transition")
	}
	if !rows[1].isOutro() {
		t.Error("the second row is not an outro")
	}
}

func TestFiftySongsYieldFortyNineTransitionsPlusAnOutro(t *testing.T) {
	state := checkPanelState(checkSongs(50, "Track"), transition.StyleOverrides{}, true)
	rows := hydrateTransitionRows("check-guild", state, state.pairs)

	if len(rows) != 50 {
		t.Errorf("got %d rows, want 50", len(rows))
	}
	if pages := transitionPageCount(state.pairs); pages != 10 {
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
		if row.isOutro() {
			outros++
			if row.fromSong.IsLive {
				t.Errorf("row %d is an outro for a live song", i)
			}
			continue
		}
		if row.fromSong.IsLive || row.toSong.IsLive {
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

	for i, row := range rows {
		if row.isOutro() {
			t.Errorf("row %d is an outro, want none for a live last song", i)
		}
	}
}

func TestSongOverrideMarksOnlyItsOwnTransition(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixStyleEffect = "echo_half_cut_end"
	rows := checkRowsFor(songs)

	if len(rows) < 2 {
		t.Fatalf("got %d rows, want at least 2", len(rows))
	}
	if got := rows[0].effective["effect"]; got != "echo_half_cut_end" {
		t.Errorf("first effect = %q, want echo_half_cut_end", got)
	}
	if got := rows[0].source["effect"]; got != "song" {
		t.Errorf("first effect source = %q, want song", got)
	}
	if got := rows[1].source["effect"]; got != "auto" {
		t.Errorf("second effect source = %q, want auto", got)
	}
}

func TestGuildDefaultsApplyWhereTheSongDoesNotOverride(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixStyleFilter = "lowpass_out"
	rows := checkRowsWithGuild(songs, transition.StyleOverrides{Filter: "lowpass_in", Effect: "reverb_out_end"})

	if len(rows) < 2 {
		t.Fatalf("got %d rows, want at least 2", len(rows))
	}
	if got := rows[0].effective["filter"]; got != "lowpass_out" {
		t.Errorf("first filter = %q, want the song override lowpass_out", got)
	}
	if got := rows[0].source["filter"]; got != "song" {
		t.Errorf("first filter source = %q, want song", got)
	}
	if got := rows[0].effective["effect"]; got != "reverb_out_end" {
		t.Errorf("first effect = %q, want the guild default reverb_out_end", got)
	}
	if got := rows[0].source["effect"]; got != "guild" {
		t.Errorf("first effect source = %q, want guild", got)
	}
	if got := rows[1].effective["filter"]; got != "lowpass_in" {
		t.Errorf("second filter = %q, want the guild default lowpass_in", got)
	}
	if got := rows[1].source["filter"]; got != "guild" {
		t.Errorf("second filter source = %q, want guild", got)
	}
}

func TestMissingAnalysisRendersAPlaceholder(t *testing.T) {
	analyzing := describeTrack("check-guild", nil, true)
	idle := describeTrack("check-guild", nil, false)

	if analyzing == "" {
		t.Error("the analyzing placeholder is empty")
	}
	if idle == "" {
		t.Error("the idle placeholder is empty")
	}
	if analyzing == idle {
		t.Errorf("both states render %q, want distinct placeholders", analyzing)
	}
}

func TestPanelHidesAutoSelectionWhenAutoMixIsOff(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixStyleEffect = "reverb_out_end"

	state := checkPanelState(songs, transition.StyleOverrides{EQ: "quick_bass"}, false)
	rows := hydrateTransitionRows("check-guild", state, state.pairs)
	if len(rows) == 0 {
		t.Fatal("no rows built")
	}

	for _, want := range []struct {
		field string
		key   string
		value string
	}{
		{"volume", "effective", "smooth"},
		{"volume", "source", "auto"},
		{"filter", "effective", "none"},
		{"effect", "effective", "reverb_out_end"},
		{"effect", "source", "song"},
		{"eq", "effective", "quick_bass"},
		{"eq", "source", "guild"},
	} {
		lookup := rows[0].effective
		if want.key == "source" {
			lookup = rows[0].source
		}
		if got := lookup[want.field]; got != want.value {
			t.Errorf("%s %s = %q, want %q", want.field, want.key, got, want.value)
		}
	}
}

func TestPanelDropsALoopThePlayerCannotRun(t *testing.T) {
	songs := checkSongs(3, "Track")
	songs[0].AutoMixStyleLoop = "eight_beats"
	rows := checkRowsFor(songs)

	if len(rows) == 0 {
		t.Fatal("no rows built")
	}
	if got := rows[0].effective["loop"]; got != "none" {
		t.Errorf("loop = %q, want none", got)
	}
	if got := rows[0].source["loop"]; got != "song" {
		t.Errorf("loop source = %q, want song", got)
	}
}

func TestAnalyzingStateFollowsTheBackfillWorker(t *testing.T) {
	songs := checkSongs(2, "Track")

	idleState := checkPanelState(songs, transition.StyleOverrides{}, true)
	idleState.backfillActive = false
	idleRows := hydrateTransitionRows("check-guild", idleState, idleState.pairs)
	busyRows := checkRowsFor(songs)

	if len(idleRows) != 2 || len(busyRows) != 2 {
		t.Fatalf("got %d idle and %d busy rows, want 2 each", len(idleRows), len(busyRows))
	}
	if idleRows[0].fromAnalyzing || idleRows[0].toAnalyzing {
		t.Error("an idle backfill worker still reported analyzing tracks")
	}
	if !busyRows[0].fromAnalyzing || !busyRows[0].toAnalyzing {
		t.Error("a busy backfill worker did not mark the transition as analyzing")
	}
	if !busyRows[1].isOutro() {
		t.Fatal("the second busy row is not an outro")
	}
	if !busyRows[1].fromAnalyzing {
		t.Error("the outro did not mark its own track as analyzing")
	}
	if busyRows[1].toAnalyzing {
		t.Error("the outro marked a nonexistent next track as analyzing")
	}
}

func TestOutroRowResolvesToTheAutoOutroRecipe(t *testing.T) {
	rows := checkRowsFor(checkSongs(2, "Track"))
	outro := rows[len(rows)-1]
	auto := transition.AutoOutroStyles(outro.fromAnalysis)

	for _, category := range transitionCategories {
		if !transition.ValidStyle(category, auto[category]) {
			t.Errorf("auto outro style %q for %q is not valid", auto[category], category)
		}
		if got := outro.effective[category]; got != auto[category] {
			t.Errorf("%s = %q, want the auto outro %q", category, got, auto[category])
		}
	}
}

func TestOutroHonoursTheLastSongsOverride(t *testing.T) {
	songs := checkSongs(2, "Track")
	songs[1].AutoMixStyleEffect = "reverb_out_center"
	rows := checkRowsFor(songs)
	last := rows[len(rows)-1]

	if !last.isOutro() {
		t.Fatal("the last row is not an outro")
	}
	if got := last.effective["effect"]; got != "reverb_out_center" {
		t.Errorf("effect = %q, want reverb_out_center", got)
	}
	if got := last.source["effect"]; got != "song" {
		t.Errorf("effect source = %q, want song", got)
	}
}
