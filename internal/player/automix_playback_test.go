package player

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/queue"
)

var styleCategories = []string{"volume", "eq", "filter", "effect", "loop"}

func containsAnnouncement(guildID string) bool {
	announcedSongsMu.Lock()
	defer announcedSongsMu.Unlock()
	_, exists := announcedSongs[guildID]
	return exists
}

func announceGuild(t *testing.T, guildID string) string {
	t.Helper()

	clearAnnounced(guildID)
	t.Cleanup(func() { clearAnnounced(guildID) })
	return guildID
}

func TestFirstAnnouncementForASongIsAllowed(t *testing.T) {
	guildID := announceGuild(t, "check-announce-first")

	if !markAnnounced(guildID, 101) {
		t.Error("the first announcement was suppressed")
	}
}

func TestRepeatAnnouncementForTheSameSongIsSuppressed(t *testing.T) {
	guildID := announceGuild(t, "check-announce-repeat")

	markAnnounced(guildID, 101)
	if markAnnounced(guildID, 101) {
		t.Error("the same song announced twice")
	}
}

func TestADifferentSongIsAnnounced(t *testing.T) {
	guildID := announceGuild(t, "check-announce-different")

	markAnnounced(guildID, 101)
	if !markAnnounced(guildID, 102) {
		t.Error("a different song was suppressed")
	}
}

func TestReturningToAnEarlierSongAnnouncesAgain(t *testing.T) {
	guildID := announceGuild(t, "check-announce-return")

	markAnnounced(guildID, 101)
	markAnnounced(guildID, 102)
	if !markAnnounced(guildID, 101) {
		t.Error("returning to song 101 was suppressed")
	}
}

func TestClearingReArmsTheSameSongForRepeatPlayback(t *testing.T) {
	guildID := announceGuild(t, "check-announce-clear")

	markAnnounced(guildID, 101)
	clearAnnounced(guildID)
	if !markAnnounced(guildID, 101) {
		t.Error("the song stayed suppressed after clearing")
	}
}

func TestAnnouncementStateIsPerGuild(t *testing.T) {
	guildID := announceGuild(t, "check-announce-guild")
	other := announceGuild(t, "check-announce-guild-other")

	markAnnounced(guildID, 101)
	if !markAnnounced(other, 101) {
		t.Error("an independent guild was suppressed")
	}
}

func TestCrossfadeThenRetryThenSeekAnnouncesExactlyOnce(t *testing.T) {
	guildID := announceGuild(t, "check-announce-once")

	total := 0
	for _, announced := range []bool{
		markAnnounced(guildID, 200),
		markAnnounced(guildID, 200),
		markAnnounced(guildID, 200),
	} {
		if announced {
			total++
		}
	}

	if total != 1 {
		t.Errorf("announced %d times, want exactly 1", total)
	}
}

func TestARemovedSongLeavesNoAnnouncementState(t *testing.T) {
	guildID := announceGuild(t, "check-announce-removed")

	if !markAnnounced(guildID, 300) {
		t.Fatal("the song was never announced")
	}
	clearAnnounced(guildID)

	if containsAnnouncement(guildID) {
		t.Error("announcement state survived the removal")
	}
}

func TestAPopulatedStreamURLIsReusedOnRestart(t *testing.T) {
	song := &queue.Song{ID: 1, URL: "https://example.invalid/watch?v=check"}

	got, err := resolveRestartStreamURL("check-restart-guild", song, false, 96000, "https://cdn.invalid/existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://cdn.invalid/existing" {
		t.Errorf("got %q, want the existing stream URL", got)
	}
}

func TestALiveSongRestartsWithoutAStreamURL(t *testing.T) {
	song := &queue.Song{ID: 2, URL: "https://example.invalid/watch?v=live", IsLive: true}

	got, err := resolveRestartStreamURL("check-restart-guild", song, false, 96000, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty stream URL", got)
	}
}

func slidingCrossfade() *crossfadeState {
	cs := newCrossfadeState()
	cs.armed = true
	cs.transitionFrame = 1000
	cs.crossfadeFrames = 400
	cs.minUsableFrames = 100
	cs.totalFrames = 1600
	cs.slideFrames = 50
	return cs
}

func TestSlidingPushesTheTransitionForwardByOneBeat(t *testing.T) {
	cs := slidingCrossfade()
	cs.slideTransition("check")

	if cs.transitionFrame != 1050 {
		t.Errorf("transitionFrame = %d, want 1050", cs.transitionFrame)
	}
}

func TestSlidingLeavesTheCrossfadeIntactWhileTheWindowStillFits(t *testing.T) {
	cs := slidingCrossfade()
	cs.slideTransition("check")

	if cs.crossfadeFrames != 400 {
		t.Errorf("crossfadeFrames = %d, want 400 with %d frames remaining", cs.crossfadeFrames, cs.totalFrames-cs.transitionFrame)
	}
}

func TestSlidingShrinksTheCrossfadeOnceItOverrunsTheWindow(t *testing.T) {
	cs := slidingCrossfade()
	cs.slideTransition("check")

	cs.transitionFrame = 1250
	cs.slideTransition("check")

	if cs.crossfadeFrames != 300 {
		t.Errorf("crossfadeFrames = %d at frame %d of %d, want 300", cs.crossfadeFrames, cs.transitionFrame, cs.totalFrames)
	}
}

func TestCrossfadeWindowNeverGrowsWhileSliding(t *testing.T) {
	cs := slidingCrossfade()
	cs.slideTransition("check")
	cs.transitionFrame = 1250
	cs.slideTransition("check")

	previous := cs.crossfadeFrames
	for i := 0; i < 40 && !cs.cancelled; i++ {
		cs.slideTransition("check")
		if cs.cancelled {
			break
		}
		if cs.crossfadeFrames > previous {
			t.Fatalf("slide %d grew the window from %d to %d", i, previous, cs.crossfadeFrames)
		}
		previous = cs.crossfadeFrames
	}
}

func TestSlidingEventuallyCancelsOnceTheWindowIsUnusable(t *testing.T) {
	cs := slidingCrossfade()
	cs.slideTransition("check")
	cs.transitionFrame = 1250
	cs.slideTransition("check")

	for i := 0; i < 40 && !cs.cancelled; i++ {
		cs.slideTransition("check")
	}

	if !cs.cancelled {
		t.Error("the crossfade never cancelled")
	}
	if cs.armed {
		t.Error("the crossfade stayed armed after cancelling")
	}
}

func TestANewCrossfadeHasNoRefetchParked(t *testing.T) {
	fresh := newCrossfadeState()

	if got := fresh.bRefetch.Load(); got != nil {
		t.Errorf("bRefetch = %v, want nil", got)
	}
	if fresh.bRefetching.Load() {
		t.Error("bRefetching is already set")
	}
	if fresh.bRetried {
		t.Error("bRetried is already set")
	}
}

func TestAbortingMarksTheRefetchAsUnwanted(t *testing.T) {
	fresh := newCrossfadeState()
	fresh.abort()

	if !fresh.bAborted.Load() {
		t.Error("bAborted was not set by abort")
	}
}

func TestTheAnalysisReadCapCoversTheFullRequestedHead(t *testing.T) {
	requested := int64(analysisHeadSecs * analysis.SampleRate * 4)

	if analysisMaxBytes <= requested {
		t.Errorf("cap %d bytes, want more than the requested %d bytes", analysisMaxBytes, requested)
	}
}

func TestTheAnalysisReadCapKeepsABoundedMargin(t *testing.T) {
	requested := int64(analysisHeadSecs * analysis.SampleRate * 4)
	margin := analysisMaxBytes - requested

	if margin <= 0 {
		t.Errorf("margin = %d bytes, want positive", margin)
	}
	if margin >= requested {
		t.Errorf("margin = %d bytes, want less than the requested %d bytes", margin, requested)
	}
}

func TestTheAnalysisReadCapAdmitsFarMoreThanTheMinimumAnalysableLength(t *testing.T) {
	minimumSamples := int64(analysis.MinSeconds * analysis.SampleRate)

	if capSamples := analysisMaxBytes / 4; capSamples <= minimumSamples {
		t.Errorf("cap %d samples, want more than the minimum %d samples", capSamples, minimumSamples)
	}
}

func styleOverridesFor(category, value string) transition.StyleOverrides {
	overrides := transition.StyleOverrides{}
	switch category {
	case "volume":
		overrides.Volume = value
	case "eq":
		overrides.EQ = value
	case "filter":
		overrides.Filter = value
	case "effect":
		overrides.Effect = value
	case "loop":
		overrides.Loop = value
	}
	return overrides
}

func resolutionAnalyses() (*analysis.TrackAnalysis, *analysis.TrackAnalysis) {
	return &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240, Tonic: 9, Minor: true, KeyConfidence: 0.5},
		&analysis.TrackAnalysis{BPM: 127, PeriodSec: 60.0 / 127, Duration: 240, Tonic: 4, Minor: true, KeyConfidence: 0.5}
}

func TestAutoStylesAreValidStyleKeys(t *testing.T) {
	analysisA, analysisB := resolutionAnalyses()
	autoStyles := transition.AutoStyles(analysisA, analysisB)

	for _, category := range styleCategories {
		if !transition.ValidStyle(category, autoStyles[category]) {
			t.Errorf("auto style %q for %q is not a valid style key", autoStyles[category], category)
		}
	}
}

func TestStylePrecedenceSongOverGuildOverAuto(t *testing.T) {
	analysisA, analysisB := resolutionAnalyses()
	autoStyles := transition.AutoStyles(analysisA, analysisB)

	for _, category := range styleCategories {
		t.Run(category, func(t *testing.T) {
			values := transition.StyleValues(category)
			guildStyle := values[len(values)-1]
			songStyle := values[1]

			cases := []struct {
				name       string
				guild      transition.StyleOverrides
				song       transition.StyleOverrides
				wantStyle  string
				wantSource string
			}{
				{"no overrides", transition.StyleOverrides{}, transition.StyleOverrides{}, autoStyles[category], "auto"},
				{"guild only", styleOverridesFor(category, guildStyle), transition.StyleOverrides{}, guildStyle, "guild"},
				{"song wins over guild", styleOverridesFor(category, guildStyle), styleOverridesFor(category, songStyle), songStyle, "song"},
				{"song auto defers to guild", styleOverridesFor(category, guildStyle), styleOverridesFor(category, transition.StyleAuto), guildStyle, "guild"},
				{"unknown values fall back to auto", styleOverridesFor(category, "not_a_real_style"), styleOverridesFor(category, ""), autoStyles[category], "auto"},
			}

			for _, testCase := range cases {
				_, effective, source := transition.ResolveStyles(analysisA, analysisB, true, testCase.guild, testCase.song)

				if effective[category] != testCase.wantStyle {
					t.Errorf("%s: style = %q, want %q", testCase.name, effective[category], testCase.wantStyle)
				}
				if source[category] != testCase.wantSource {
					t.Errorf("%s: source = %q, want %q", testCase.name, source[category], testCase.wantSource)
				}
			}
		})
	}
}

func TestOverrideAffectsOnlyItsOwnCategory(t *testing.T) {
	analysisA, analysisB := resolutionAnalyses()
	autoStyles := transition.AutoStyles(analysisA, analysisB)

	_, effective, source := transition.ResolveStyles(analysisA, analysisB, true,
		transition.StyleOverrides{}, transition.StyleOverrides{Effect: "echo_half_cut_end"})

	if effective["effect"] != "echo_half_cut_end" {
		t.Errorf("effect = %q, want echo_half_cut_end", effective["effect"])
	}
	for _, category := range styleCategories {
		if category == "effect" {
			continue
		}
		if effective[category] != autoStyles[category] {
			t.Errorf("%s = %q, want the untouched auto style %q", category, effective[category], autoStyles[category])
		}
		if source[category] != "auto" {
			t.Errorf("%s source = %q, want auto", category, source[category])
		}
	}
}

func TestNilAnalysisResolvesToTheDefaultRecipe(t *testing.T) {
	_, effective, source := transition.ResolveStyles(nil, nil, true, transition.StyleOverrides{}, transition.StyleOverrides{})

	for category, want := range map[string]string{
		"volume": "smooth",
		"eq":     "none",
		"filter": "none",
		"effect": "none",
		"loop":   "none",
	} {
		if effective[category] != want {
			t.Errorf("%s = %q, want %q", category, effective[category], want)
		}
	}
	if source["volume"] != "auto" {
		t.Errorf("volume source = %q, want auto", source["volume"])
	}
}

func TestAutoSelectionIsSkippedWhenDisabled(t *testing.T) {
	analysisA, analysisB := resolutionAnalyses()
	defaultStyles := transition.RecipeStyleMap(transition.DefaultRecipe())

	_, effective, source := transition.ResolveStyles(analysisA, analysisB, false, transition.StyleOverrides{}, transition.StyleOverrides{})

	for _, category := range styleCategories {
		if effective[category] != defaultStyles[category] {
			t.Errorf("%s = %q, want the default %q", category, effective[category], defaultStyles[category])
		}
		if source[category] != "auto" {
			t.Errorf("%s source = %q, want auto", category, source[category])
		}
	}
	if reflect.DeepEqual(effective, transition.AutoStyles(analysisA, analysisB)) {
		t.Error("disabled auto selection still produced the auto styles")
	}
}

func TestOverridesStillLayerWhenAutoSelectionIsDisabled(t *testing.T) {
	analysisA, analysisB := resolutionAnalyses()
	defaultStyles := transition.RecipeStyleMap(transition.DefaultRecipe())

	_, effective, source := transition.ResolveStyles(analysisA, analysisB, false,
		transition.StyleOverrides{EQ: "quick_bass"}, transition.StyleOverrides{Effect: "reverb_out_end"})

	for _, want := range []struct{ category, style, source string }{
		{"eq", "quick_bass", "guild"},
		{"effect", "reverb_out_end", "song"},
		{"volume", defaultStyles["volume"], "auto"},
	} {
		if effective[want.category] != want.style {
			t.Errorf("%s = %q, want %q", want.category, effective[want.category], want.style)
		}
		if source[want.category] != want.source {
			t.Errorf("%s source = %q, want %q", want.category, source[want.category], want.source)
		}
	}
}

func outroAnalysisShapes() []struct {
	name  string
	track *analysis.TrackAnalysis
} {
	return []struct {
		name  string
		track *analysis.TrackAnalysis
	}{
		{"nil analysis", nil},
		{"zero bpm", &analysis.TrackAnalysis{BPM: 0, PeriodSec: 0}},
		{"bpm without grid", &analysis.TrackAnalysis{BPM: 128, PeriodSec: 0}},
		{"full grid", &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Tonic: 9, Minor: true, KeyConfidence: 0.5}},
	}
}

func TestOutroRecipesAreValidAcrossEveryAnalysisShape(t *testing.T) {
	for _, input := range outroAnalysisShapes() {
		styles := transition.AutoOutroStyles(input.track)
		for _, category := range styleCategories {
			if !transition.ValidStyle(category, styles[category]) {
				t.Errorf("%s: %q style %q is not a valid style key", input.name, category, styles[category])
			}
		}
	}
}

func TestEveryOutroShapesTheEnding(t *testing.T) {
	for _, input := range outroAnalysisShapes() {
		styles := transition.AutoOutroStyles(input.track)
		if styles["effect"] == "none" && styles["filter"] == "none" {
			t.Errorf("%s produced no filter and no effect", input.name)
		}
	}
}

func TestOutroDiffersFromTheOrdinaryTransitionRecipe(t *testing.T) {
	gridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}

	outro := transition.AutoOutroStyles(gridded)
	ordinary := transition.AutoStyles(gridded, gridded)
	if reflect.DeepEqual(outro, ordinary) {
		t.Errorf("outro %v matches the ordinary transition %v", outro, ordinary)
	}
}

func TestABeatGridEarnsARhythmicOutro(t *testing.T) {
	gridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}
	ungridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 0}

	if got := transition.AutoOutroStyles(gridded)["effect"]; got != "echo_half_cut_end" {
		t.Errorf("gridded effect = %q, want echo_half_cut_end", got)
	}
	if got := transition.AutoOutroStyles(ungridded)["effect"]; got == "echo_half_cut_end" {
		t.Errorf("ungridded effect = %q, want anything but the rhythmic echo", got)
	}
}

func TestOutroAutoSelectionIsSkippedWhenDisabled(t *testing.T) {
	gridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}
	defaultStyles := transition.RecipeStyleMap(transition.DefaultRecipe())

	_, effective, source := transition.ResolveOutroStyles(gridded, false, transition.StyleOverrides{}, transition.StyleOverrides{})

	for _, category := range styleCategories {
		if effective[category] != defaultStyles[category] {
			t.Errorf("%s = %q, want the default %q", category, effective[category], defaultStyles[category])
		}
		if source[category] != "auto" {
			t.Errorf("%s source = %q, want auto", category, source[category])
		}
	}
}

func TestOutroOverridesLayerSongOverGuildOverAuto(t *testing.T) {
	gridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}
	auto := transition.AutoOutroStyles(gridded)

	_, effective, source := transition.ResolveOutroStyles(gridded, true,
		transition.StyleOverrides{Volume: "fadein_cutout"}, transition.StyleOverrides{Effect: "reverb_out_center"})

	for _, want := range []struct{ category, style, source string }{
		{"volume", "fadein_cutout", "guild"},
		{"effect", "reverb_out_center", "song"},
		{"filter", auto["filter"], "auto"},
	} {
		if effective[want.category] != want.style {
			t.Errorf("%s = %q, want %q", want.category, effective[want.category], want.style)
		}
		if source[want.category] != want.source {
			t.Errorf("%s source = %q, want %q", want.category, source[want.category], want.source)
		}
	}
}

func TestEveryVolumeStyleGivesTheOutroADistinctShape(t *testing.T) {
	distinct := map[string]bool{}

	for _, style := range transition.StyleValues("volume") {
		if style == transition.StyleAuto {
			continue
		}

		recipe := transition.ApplyStyleOverrides(transition.DefaultRecipe(), transition.StyleOverrides{Volume: style})
		processor := transition.NewProcessor(recipe, 500, 60.0/128)
		start, _ := processor.Gains(0)
		mid, _ := processor.Gains(0.5)
		end, _ := processor.Gains(1)
		distinct[fmt.Sprintf("%.3f/%.3f/%.3f", start, mid, end)] = true
	}

	if len(distinct) != 5 {
		t.Errorf("got %d distinct A-side curves across 5 styles, want 5", len(distinct))
	}
}

func outroWindowFixture() (*ffmpeg.EndState, fadeSettings, int) {
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
	fade := fadeSettings{autoMix: true, autoMixBeats: 16, crossfadeSec: 8}
	expectedFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	return &ffmpeg.EndState{TotalFrames: 12000, Analysis: track}, fade, expectedFrames
}

func TestOutroWindowSitsAtTheEndOfTheTrack(t *testing.T) {
	full, fade, expectedFrames := outroWindowFixture()

	start, frames, ok := planOutroWindow(full, 100, fade)
	if !ok {
		t.Fatal("no outro window was planned")
	}
	if frames != expectedFrames {
		t.Errorf("frames = %d, want %d", frames, expectedFrames)
	}
	if want := full.TotalFrames - expectedFrames; start != want {
		t.Errorf("start = %d, want %d", start, want)
	}
	if start <= 100 {
		t.Errorf("start = %d, want it after the current frame 100", start)
	}
}

func TestOutroWindowRespectsTheTrimmedSilentTail(t *testing.T) {
	_, fade, expectedFrames := outroWindowFixture()
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
	trimmed := &ffmpeg.EndState{TotalFrames: 12000, SilentTailFrames: 500, Analysis: track}

	start, _, ok := planOutroWindow(trimmed, 100, fade)
	if !ok {
		t.Fatal("no outro window was planned")
	}
	if want := 12000 - 500 - expectedFrames; start != want {
		t.Errorf("start = %d, want %d", start, want)
	}
}

func TestOutroRefusesAWindowThatStartsInThePast(t *testing.T) {
	full, fade, expectedFrames := outroWindowFixture()

	if _, _, ok := planOutroWindow(full, full.TotalFrames-expectedFrames, fade); ok {
		t.Error("a window starting in the past was accepted")
	}
}

func TestOutroRefusesATrackTooShortToHoldIt(t *testing.T) {
	_, fade, _ := outroWindowFixture()
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}

	if _, _, ok := planOutroWindow(&ffmpeg.EndState{TotalFrames: 200, Analysis: track}, 100, fade); ok {
		t.Error("a 200 frame track was accepted")
	}
}

func TestOutroWindowStillResolvesWithoutAnalysis(t *testing.T) {
	full, fade, _ := outroWindowFixture()

	if _, _, ok := planOutroWindow(full, 100, fadeSettings{autoMix: false, autoMixBeats: 16, crossfadeSec: 8}); !ok {
		t.Error("no window was planned with automix off")
	}

	start, frames, ok := planOutroWindow(&ffmpeg.EndState{TotalFrames: 12000}, 100, fade)
	if !ok {
		t.Fatal("no window was planned without analysis")
	}
	if want := int(transition.FallbackCrossfadeSec * dsp.FramesPerSecond); frames != want {
		t.Errorf("frames = %d, want the fallback %d", frames, want)
	}
	if start <= 100 {
		t.Errorf("start = %d, want it after the current frame 100", start)
	}
}

func timingTrack() *analysis.TrackAnalysis {
	return &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
}

func TestCrossfadeFramesFollowTheConfiguredDurationWhenAutoMixIsOff(t *testing.T) {
	frames, seconds := transition.CrossfadeFrames(false, 16, 8, timingTrack())

	if want := int(8 * dsp.FramesPerSecond); frames != want {
		t.Errorf("frames = %d, want %d", frames, want)
	}
	if seconds != 8 {
		t.Errorf("seconds = %.2f, want 8", seconds)
	}
}

func TestCrossfadeFramesFollowTheBeatGridWhenAutoMixIsOn(t *testing.T) {
	track := timingTrack()
	frames, seconds := transition.CrossfadeFrames(true, 16, 8, track)
	beatDerived := 16 * track.PeriodSec

	if math.Abs(seconds-beatDerived) >= 0.001 {
		t.Errorf("seconds = %.3f, want the beat-derived %.3f", seconds, beatDerived)
	}
	if want := int(seconds * dsp.FramesPerSecond); frames != want {
		t.Errorf("frames = %d, want %d", frames, want)
	}
}

func TestCrossfadeFramesFallBackWithoutAnalysis(t *testing.T) {
	frames, _ := transition.CrossfadeFrames(true, 16, 0, nil)

	if want := int(transition.FallbackCrossfadeSec * dsp.FramesPerSecond); frames != want {
		t.Errorf("frames = %d, want the fallback %d", frames, want)
	}
}

func TestCrossfadeSecondsAreClampedToTheMaximum(t *testing.T) {
	_, seconds := transition.CrossfadeFrames(true, 4096, 8, timingTrack())

	if seconds != transition.CrossfadeMaxSec {
		t.Errorf("seconds = %.2f, want the maximum %.2f", seconds, transition.CrossfadeMaxSec)
	}
}

func TestLoopSurvivesWhenItFitsInsideTheCrossfade(t *testing.T) {
	track := timingTrack()
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	style, loopFrames := transition.ClampLoopStyle(transition.LoopFourBeats, track.PeriodSec, crossfadeFrames)
	if style != transition.LoopFourBeats {
		t.Errorf("style = %s, want four_beats", style)
	}
	if loopFrames <= 0 {
		t.Errorf("loopFrames = %d of %d, want positive", loopFrames, crossfadeFrames)
	}
}

func TestLoopIsDroppedWhenItNeedsMoreThanHalfTheCrossfade(t *testing.T) {
	track := timingTrack()
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	style, loopFrames := transition.ClampLoopStyle(transition.LoopEightBeats, track.PeriodSec, crossfadeFrames)
	if style != transition.LoopNone {
		t.Errorf("style = %s, want none", style)
	}
	if loopFrames != 0 {
		t.Errorf("loopFrames = %d of %d, want 0", loopFrames, crossfadeFrames)
	}
}

func TestLoopIsDroppedWithoutABeatGrid(t *testing.T) {
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, timingTrack())

	style, loopFrames := transition.ClampLoopStyle(transition.LoopFourBeats, 0, crossfadeFrames)
	if style != transition.LoopNone {
		t.Errorf("style = %s, want none", style)
	}
	if loopFrames != 0 {
		t.Errorf("loopFrames = %d, want 0", loopFrames)
	}
}

func TestLoopNoneStaysNone(t *testing.T) {
	track := timingTrack()
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	style, loopFrames := transition.ClampLoopStyle(transition.LoopNone, track.PeriodSec, crossfadeFrames)
	if style != transition.LoopNone {
		t.Errorf("style = %s, want none", style)
	}
	if loopFrames != 0 {
		t.Errorf("loopFrames = %d, want 0", loopFrames)
	}
}

func TestAnalysisSummaryHandlesNil(t *testing.T) {
	bpm, key, camelot, hasKey := analysis.Summarize(nil)

	if bpm != 0 {
		t.Errorf("bpm = %.1f, want 0", bpm)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
	if camelot != "" {
		t.Errorf("camelot = %q, want empty", camelot)
	}
	if hasKey {
		t.Error("hasKey is true for a nil analysis")
	}
}

func TestAnalysisSummaryHidesLowConfidenceKeys(t *testing.T) {
	track := &analysis.TrackAnalysis{BPM: 120, Tonic: 3, Minor: false, KeyConfidence: analysis.KeyConfidenceFloor / 2}
	bpm, _, _, hasKey := analysis.Summarize(track)

	if bpm != 120 {
		t.Errorf("bpm = %.1f, want 120", bpm)
	}
	if hasKey {
		t.Errorf("hasKey is true at confidence %.4f, want it hidden below the floor %.4f", track.KeyConfidence, analysis.KeyConfidenceFloor)
	}
}

func TestAnalysisSummaryReportsConfidentKeys(t *testing.T) {
	_, key, camelot, hasKey := analysis.Summarize(&analysis.TrackAnalysis{BPM: 174, Tonic: 0, Minor: false, KeyConfidence: 0.5})

	if !hasKey {
		t.Fatal("hasKey is false for a confident key")
	}
	if key != "C major" {
		t.Errorf("key = %q, want C major", key)
	}
	if camelot != "8B" {
		t.Errorf("camelot = %q, want 8B", camelot)
	}
}

func TestCompatibilityRejectsNilInput(t *testing.T) {
	confident := &analysis.TrackAnalysis{BPM: 174, Tonic: 0, Minor: false, KeyConfidence: 0.5}

	_, distance, ok := analysis.Compare(nil, confident)
	if ok {
		t.Error("a nil analysis was accepted")
	}
	if distance != -1 {
		t.Errorf("distance = %d, want -1", distance)
	}
}

func TestCompatibilityRejectsZeroBPM(t *testing.T) {
	confident := &analysis.TrackAnalysis{BPM: 174, Tonic: 0, Minor: false, KeyConfidence: 0.5}

	if _, _, ok := analysis.Compare(&analysis.TrackAnalysis{BPM: 0, KeyConfidence: 0.5}, confident); ok {
		t.Error("a zero BPM analysis was accepted")
	}
}

func TestCompatibilityReportsSignedBPMDelta(t *testing.T) {
	slower := &analysis.TrackAnalysis{BPM: 120, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	faster := &analysis.TrackAnalysis{BPM: 132, Tonic: 0, Minor: false, KeyConfidence: 0.5}

	delta, distance, ok := analysis.Compare(slower, faster)
	if !ok {
		t.Fatal("two valid analyses were rejected")
	}
	if math.Abs(delta-0.1) >= 1e-9 {
		t.Errorf("delta = %.4f, want 0.1", delta)
	}
	if distance != 0 {
		t.Errorf("distance = %d, want 0", distance)
	}
}
