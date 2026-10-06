package player_test

import (
	"fmt"
	"math"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/audiotest"
)

func containsAnnouncement(guildID string) bool {
	player.HookAnnouncedSongsMu.Lock()
	defer player.HookAnnouncedSongsMu.Unlock()
	_, exists := (*player.HookAnnouncedSongs)[guildID]
	return exists
}

func announceGuild(t *testing.T, guildID string) string {
	t.Helper()

	player.HookClearAnnounced(guildID)
	t.Cleanup(func() { player.HookClearAnnounced(guildID) })
	return guildID
}

func TestFirstAnnouncementForASongIsAllowed(t *testing.T) {
	guildID := announceGuild(t, "check-announce-first")

	if !player.HookMarkAnnounced(guildID, 101) {
		t.Error("the first announcement was suppressed")
	}
}

func TestRepeatAnnouncementForTheSameSongIsSuppressed(t *testing.T) {
	guildID := announceGuild(t, "check-announce-repeat")

	player.HookMarkAnnounced(guildID, 101)
	if player.HookMarkAnnounced(guildID, 101) {
		t.Error("the same song announced twice")
	}
}

func TestADifferentSongIsAnnounced(t *testing.T) {
	guildID := announceGuild(t, "check-announce-different")

	player.HookMarkAnnounced(guildID, 101)
	if !player.HookMarkAnnounced(guildID, 102) {
		t.Error("a different song was suppressed")
	}
}

func TestReturningToAnEarlierSongAnnouncesAgain(t *testing.T) {
	guildID := announceGuild(t, "check-announce-return")

	player.HookMarkAnnounced(guildID, 101)
	player.HookMarkAnnounced(guildID, 102)
	if !player.HookMarkAnnounced(guildID, 101) {
		t.Error("returning to song 101 was suppressed")
	}
}

func TestClearingReArmsTheSameSongForRepeatPlayback(t *testing.T) {
	guildID := announceGuild(t, "check-announce-clear")

	player.HookMarkAnnounced(guildID, 101)
	player.HookClearAnnounced(guildID)
	if !player.HookMarkAnnounced(guildID, 101) {
		t.Error("the song stayed suppressed after clearing")
	}
}

func TestAnnouncementStateIsPerGuild(t *testing.T) {
	guildID := announceGuild(t, "check-announce-guild")
	other := announceGuild(t, "check-announce-guild-other")

	player.HookMarkAnnounced(guildID, 101)
	if !player.HookMarkAnnounced(other, 101) {
		t.Error("an independent guild was suppressed")
	}
}

func TestCrossfadeThenRetryThenSeekAnnouncesExactlyOnce(t *testing.T) {
	guildID := announceGuild(t, "check-announce-once")

	total := 0
	for _, announced := range []bool{
		player.HookMarkAnnounced(guildID, 200),
		player.HookMarkAnnounced(guildID, 200),
		player.HookMarkAnnounced(guildID, 200),
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

	if !player.HookMarkAnnounced(guildID, 300) {
		t.Fatal("the song was never announced")
	}
	player.HookClearAnnounced(guildID)

	if containsAnnouncement(guildID) {
		t.Error("announcement state survived the removal")
	}
}

func TestAPopulatedStreamURLIsReusedOnRestart(t *testing.T) {
	song := &queue.Song{ID: 1, URL: "https://example.invalid/watch?v=check"}

	got, err := player.HookResolveRestartStreamURL("check-restart-guild", song, false, 96000, "https://cdn.invalid/existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://cdn.invalid/existing" {
		t.Errorf("got %q, want the existing stream URL", got)
	}
}

func TestALiveSongRestartsWithoutAStreamURL(t *testing.T) {
	song := &queue.Song{ID: 2, URL: "https://example.invalid/watch?v=live", IsLive: true}

	got, err := player.HookResolveRestartStreamURL("check-restart-guild", song, false, 96000, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty stream URL", got)
	}
}

func slidingCrossfade() *player.HookCrossfadeState {
	cs := player.HookNewCrossfadeState()
	*cs.HookArmed() = true
	*cs.HookTransitionFrame() = 1000
	*cs.HookCrossfadeFrames() = 400
	*cs.HookMinUsableFrames() = 100
	*cs.HookTotalFrames() = 1600
	*cs.HookSlideFrames() = 50
	return cs
}

func TestSlidingPushesTheTransitionForwardByOneBeat(t *testing.T) {
	cs := slidingCrossfade()
	cs.HookSlideTransition("check")

	if *cs.HookTransitionFrame() != 1050 {
		t.Errorf("transitionFrame = %d, want 1050", *cs.HookTransitionFrame())
	}
}

func TestSlidingLeavesTheCrossfadeIntactWhileTheWindowStillFits(t *testing.T) {
	cs := slidingCrossfade()
	cs.HookSlideTransition("check")

	if *cs.HookCrossfadeFrames() != 400 {
		t.Errorf("crossfadeFrames = %d, want 400 with %d frames remaining", *cs.HookCrossfadeFrames(), *cs.HookTotalFrames()-*cs.HookTransitionFrame())
	}
}

func TestSlidingShrinksTheCrossfadeOnceItOverrunsTheWindow(t *testing.T) {
	cs := slidingCrossfade()
	cs.HookSlideTransition("check")

	*cs.HookTransitionFrame() = 1250
	cs.HookSlideTransition("check")

	if *cs.HookCrossfadeFrames() != 300 {
		t.Errorf("crossfadeFrames = %d at frame %d of %d, want 300", *cs.HookCrossfadeFrames(), *cs.HookTransitionFrame(), *cs.HookTotalFrames())
	}
}

func TestCrossfadeWindowNeverGrowsWhileSliding(t *testing.T) {
	cs := slidingCrossfade()
	cs.HookSlideTransition("check")
	*cs.HookTransitionFrame() = 1250
	cs.HookSlideTransition("check")

	previous := *cs.HookCrossfadeFrames()
	for i := 0; i < 40 && !*cs.HookCancelled(); i++ {
		cs.HookSlideTransition("check")
		if *cs.HookCancelled() {
			break
		}
		if *cs.HookCrossfadeFrames() > previous {
			t.Fatalf("slide %d grew the window from %d to %d", i, previous, *cs.HookCrossfadeFrames())
		}
		previous = *cs.HookCrossfadeFrames()
	}
}

func TestSlidingEventuallyCancelsOnceTheWindowIsUnusable(t *testing.T) {
	cs := slidingCrossfade()
	cs.HookSlideTransition("check")
	*cs.HookTransitionFrame() = 1250
	cs.HookSlideTransition("check")

	for i := 0; i < 40 && !*cs.HookCancelled(); i++ {
		cs.HookSlideTransition("check")
	}

	if !*cs.HookCancelled() {
		t.Error("the crossfade never cancelled")
	}
	if *cs.HookArmed() {
		t.Error("the crossfade stayed armed after cancelling")
	}
}

func TestANewCrossfadeHasNoRefetchParked(t *testing.T) {
	fresh := player.HookNewCrossfadeState()

	if got := fresh.HookBRefetch().Load(); got != nil {
		t.Errorf("bRefetch = %v, want nil", got)
	}
	if fresh.HookBRefetching().Load() {
		t.Error("bRefetching is already set")
	}
	if *fresh.HookBRetried() {
		t.Error("bRetried is already set")
	}
}

func TestAbortingMarksTheRefetchAsUnwanted(t *testing.T) {
	fresh := player.HookNewCrossfadeState()
	fresh.HookAbort()

	if !fresh.HookBAborted().Load() {
		t.Error("bAborted was not set by abort")
	}
}

func TestTheAnalysisReadCapCoversTheFullRequestedHead(t *testing.T) {
	requested := int64(player.HookAnalysisHeadSecs * analysis.SampleRate * 4)

	if player.HookAnalysisMaxBytes <= requested {
		t.Errorf("cap %d bytes, want more than the requested %d bytes", player.HookAnalysisMaxBytes, requested)
	}
}

func TestTheAnalysisReadCapKeepsABoundedMargin(t *testing.T) {
	requested := int64(player.HookAnalysisHeadSecs * analysis.SampleRate * 4)
	margin := player.HookAnalysisMaxBytes - requested

	if margin <= 0 {
		t.Errorf("margin = %d bytes, want positive", margin)
	}
	if margin >= requested {
		t.Errorf("margin = %d bytes, want less than the requested %d bytes", margin, requested)
	}
}

func TestTheAnalysisReadCapAdmitsFarMoreThanTheMinimumAnalysableLength(t *testing.T) {
	minimumSamples := int64(analysis.MinSeconds * analysis.SampleRate)

	if capSamples := player.HookAnalysisMaxBytes / 4; capSamples <= minimumSamples {
		t.Errorf("cap %d samples, want more than the minimum %d samples", capSamples, minimumSamples)
	}
}

var autoRecipe = transition.PresetRecipe(3)

type expectedStyle struct {
	category transition.Category
	style    string
	source   string
}

func checkResolved(t *testing.T, resolved *transition.Resolved, wants []expectedStyle) {
	t.Helper()
	for _, want := range wants {
		if got := transition.StyleOf(&resolved.Recipe, want.category); got != want.style {
			t.Errorf("%s = %q, want %q", want.category, got, want.style)
		}
		if got := resolved.Sources[want.category]; got != want.source {
			t.Errorf("%s source = %q, want %q", want.category, got, want.source)
		}
	}
}

func TestEveryPresetResolvesToValidStyleKeys(t *testing.T) {
	for _, preset := range []int{1, 2, 3, 4, 5, 8, 9, 10, 11, 17, 18, 19} {
		for _, category := range transition.StyleCategories() {
			if style := transition.StyleOf(transition.PresetRecipe(preset), category); !transition.ValidStyle(category, style) {
				t.Errorf("preset %d style %q for %q is not a valid style key", preset, style, category)
			}
		}
	}
}

func TestStylePrecedenceSongOverGuildOverAuto(t *testing.T) {
	for _, category := range transition.StyleCategories() {
		t.Run(string(category), func(t *testing.T) {
			values := transition.StyleValues(category)
			guildStyle := values[len(values)-1]
			songStyle := values[1]
			autoStyle := transition.StyleOf(autoRecipe, category)

			cases := []struct {
				name       string
				guild      map[string]string
				song       map[string]string
				wantStyle  string
				wantSource string
			}{
				{"no overrides", nil, nil, autoStyle, "auto"},
				{"guild only", category.Override(guildStyle), nil, guildStyle, "guild"},
				{"song wins over guild", category.Override(guildStyle), category.Override(songStyle), songStyle, "song"},
				{"song auto defers to guild", category.Override(guildStyle), category.Override(transition.StyleAuto), guildStyle, "guild"},
				{"unknown values fall back to auto", category.Override("not_a_real_style"), category.Override(""), autoStyle, "auto"},
			}

			for _, testCase := range cases {
				resolved := transition.ResolveStyles(autoRecipe, testCase.guild, testCase.song)
				if got := transition.StyleOf(&resolved.Recipe, category); got != testCase.wantStyle {
					t.Errorf("%s: style = %q, want %q", testCase.name, got, testCase.wantStyle)
				}
				if got := resolved.Sources[category]; got != testCase.wantSource {
					t.Errorf("%s: source = %q, want %q", testCase.name, got, testCase.wantSource)
				}
			}
		})
	}
}

func TestOverrideAffectsOnlyItsOwnCategory(t *testing.T) {
	resolved := transition.ResolveStyles(autoRecipe, nil, transition.CategoryFXOut.Override("echo_half_cut_end"))

	for _, category := range transition.StyleCategories() {
		want := expectedStyle{category, transition.StyleOf(autoRecipe, category), "auto"}
		if category == transition.CategoryFXOut {
			want = expectedStyle{category, "echo_half_cut_end", "song"}
		}
		checkResolved(t, resolved, []expectedStyle{want})
	}
}

func TestTheFadePresetIsTheDefaultCrossfadeWithABassSwap(t *testing.T) {
	resolved := transition.ResolveStyles(transition.PresetRecipe(transition.FadePreset), nil, nil)

	checkResolved(t, resolved, []expectedStyle{
		{transition.CategoryVolumeOut, "cross_shape", "auto"},
		{transition.CategoryVolumeIn, "cross_shape", "auto"},
		{transition.CategoryEQOut, "bass_fast", "auto"},
		{transition.CategoryEQIn, "bass_fast", "auto"},
		{transition.CategoryFilterOut, "none", "auto"},
		{transition.CategoryFXOut, "none", "auto"},
		{transition.CategoryLoop, "none", "auto"},
	})
}

func TestOverridesStillLayerOverThePlainCrossfade(t *testing.T) {
	resolved := transition.ResolveStyles(transition.PresetRecipe(transition.NoPreset),
		transition.ExpandLegacy(transition.ShortcutEQ, "quick_bass"), transition.CategoryFXOut.Override("reverb_out_end"))

	checkResolved(t, resolved, []expectedStyle{
		{transition.CategoryEQOut, "bass_fast_one_bar_from_end", "guild"},
		{transition.CategoryFXOut, "reverb_out_end", "song"},
		{transition.CategoryVolumeOut, "cross_shape", "auto"},
	})
}

func TestAutoOutroLetsTheSongEndNaturally(t *testing.T) {
	outro := transition.OutroRecipe()
	for _, category := range []transition.Category{transition.CategoryEQOut, transition.CategoryFilterOut, transition.CategoryFXOut, transition.CategoryLoop} {
		if style := transition.StyleOf(outro, category); style != "none" {
			t.Errorf("%s = %q, want none so the last song is left alone", category, style)
		}
	}
	if !outro.IsOutroDefault() {
		t.Error("the auto outro is not recognized as the default")
	}
}

func TestOutroOverridesLayerSongOverGuildOverAuto(t *testing.T) {
	resolved := transition.ResolveStyles(transition.OutroRecipe(),
		transition.ExpandLegacy(transition.ShortcutVolume, "fadein_cutout"), transition.CategoryFXOut.Override("reverb_out_center"))

	checkResolved(t, resolved, []expectedStyle{
		{transition.CategoryVolumeOut, "fast_at_end", "guild"},
		{transition.CategoryFXOut, "reverb_out_center", "song"},
		{transition.CategoryFilterOut, "none", "auto"},
	})
}

func TestEveryVolumeStyleGivesTheOutroADistinctShape(t *testing.T) {
	distinct := map[string]bool{}

	for _, style := range transition.StyleValues(transition.CategoryVolumeOut)[1:] {
		recipe := transition.DefaultRecipe()
		recipe.Apply(transition.CategoryVolumeOut.Override(style))
		processor := transition.NewProcessor(&recipe, &transition.Window{Frames: 500, PeriodSec: 60.0 / 128, Bars: 8})
		samples := make([]string, 0, 9)
		for _, progress := range []float64{0, 0.25, 0.5, 0.505, 0.6, 0.8, 0.95, 0.999, 1} {
			gain, _ := processor.Gains(progress)
			samples = append(samples, fmt.Sprintf("%.3f", gain))
		}
		distinct[fmt.Sprint(samples)] = true
	}

	if want := len(transition.StyleValues(transition.CategoryVolumeOut)) - 1; len(distinct) != want {
		t.Errorf("got %d distinct outgoing curves, want %d: every outgoing volume style has its own shape", len(distinct), want)
	}
}

func outroWindowFixture() (*ffmpeg.EndState, *player.HookFadeSettings, int) {
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
	fade := player.HookBuildFadeSettings(player.HookFadeSettingsFields{AutoMix: true, AutoMixBeats: 16, CrossfadeSec: 8})
	expectedFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	return &ffmpeg.EndState{TotalFrames: 12000, Analysis: track}, fade, expectedFrames
}

func TestOutroWindowSitsAtTheEndOfTheTrack(t *testing.T) {
	full, fade, expectedFrames := outroWindowFixture()

	start, frames, ok := player.HookPlanOutroWindow(full, 100, fade)
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

	start, _, ok := player.HookPlanOutroWindow(trimmed, 100, fade)
	if !ok {
		t.Fatal("no outro window was planned")
	}
	if want := 12000 - 500 - expectedFrames; start != want {
		t.Errorf("start = %d, want %d", start, want)
	}
}

func TestOutroRefusesAWindowThatStartsInThePast(t *testing.T) {
	full, fade, expectedFrames := outroWindowFixture()

	if _, _, ok := player.HookPlanOutroWindow(full, full.TotalFrames-expectedFrames, fade); ok {
		t.Error("a window starting in the past was accepted")
	}
}

func TestOutroRefusesATrackTooShortToHoldIt(t *testing.T) {
	_, fade, _ := outroWindowFixture()
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}

	if _, _, ok := player.HookPlanOutroWindow(&ffmpeg.EndState{TotalFrames: 200, Analysis: track}, 100, fade); ok {
		t.Error("a 200 frame track was accepted")
	}
}

func TestOutroWindowStillResolvesWithoutAnalysis(t *testing.T) {
	full, fade, _ := outroWindowFixture()

	if _, _, ok := player.HookPlanOutroWindow(full, 100, player.HookBuildFadeSettings(player.HookFadeSettingsFields{AutoMix: false, AutoMixBeats: 16, CrossfadeSec: 8})); !ok {
		t.Error("no window was planned with automix off")
	}

	start, frames, ok := player.HookPlanOutroWindow(&ffmpeg.EndState{TotalFrames: 12000}, 100, fade)
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

func loopOutput(loop *transition.BeatLoop, frames int) []int16 {
	output := make([]int16, 0, frames*dsp.FrameSize)
	frame := make([]int16, dsp.FrameSize*dsp.Channels)
	for index := 0; index < frames; index++ {
		for pair := 0; pair < dsp.FrameSize; pair++ {
			value := int16((index*dsp.FrameSize + pair) % 30000)
			frame[pair*dsp.Channels] = value
			frame[pair*dsp.Channels+1] = value
		}
		out := loop.Next(frame)
		for pair := 0; pair < dsp.FrameSize; pair++ {
			output = append(output, out[pair*dsp.Channels])
		}
	}
	return output
}

func TestLoopSurvivesWhenItFitsInsideTheCrossfade(t *testing.T) {
	track := timingTrack()
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	if style := clampedLoop(transition.LoopFourBeats, track.PeriodSec, crossfadeFrames); style != transition.LoopFourBeats {
		t.Errorf("style = %d, want four_beats", style)
	}
	output := loopOutput(transition.PrepareBeatLoop(transition.LoopFourBeats, track.PeriodSec, crossfadeFrames), 100)
	if got := output[90000+500]; got != 500 {
		t.Errorf("sample 90500 = %d, want 500 from a 90000-sample loop (four beats at 128 BPM, not rounded to whole frames)", got)
	}
}

func TestLoopLengthRoundsToTheNearestSample(t *testing.T) {
	track := &analysis.TrackAnalysis{BPM: 123, PeriodSec: 60.0 / 123, Duration: 240}
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	output := loopOutput(transition.PrepareBeatLoop(transition.LoopFourBeats, track.PeriodSec, crossfadeFrames), 100)
	if got := output[93659+500]; got != 500 {
		t.Errorf("sample 94159 = %d, want 500 from a 93659-sample loop (93658.5 rounded)", got)
	}
}

func TestLoopIsDroppedWhenItIsLongerThanTheCrossfade(t *testing.T) {
	track := timingTrack()
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 8, 8, track)

	if style := clampedLoop(transition.LoopSixteenBeats, track.PeriodSec, crossfadeFrames); style != transition.LoopNone {
		t.Errorf("style = %d, want none", style)
	}
	if loop := transition.PrepareBeatLoop(transition.LoopSixteenBeats, track.PeriodSec, crossfadeFrames); loop != nil {
		t.Error("a loop longer than the crossfade was still built")
	}
}

func TestLoopIsDroppedWithoutABeatGrid(t *testing.T) {
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, timingTrack())

	if style := clampedLoop(transition.LoopFourBeats, 0, crossfadeFrames); style != transition.LoopNone {
		t.Errorf("style = %d, want none", style)
	}
}

func TestLoopNoneStaysNone(t *testing.T) {
	track := timingTrack()
	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	if style := clampedLoop(transition.LoopNone, track.PeriodSec, crossfadeFrames); style != transition.LoopNone {
		t.Errorf("style = %d, want none", style)
	}
}

func clampedLoop(loop transition.LoopStyle, periodSec float64, frames int) transition.LoopStyle {
	resolved := transition.ResolveStyles(&transition.Recipe{Loop: loop}, nil, nil)
	resolved.ClampRolls(periodSec, frames)
	return resolved.Recipe.Loop
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

func TestTransitionSnappingLandsOnTheGrid(t *testing.T) {
	track := audiotest.AnalyzeAccentedClickTrack(t)

	grid := player.HookSnapTransitionToGrid(1000, 0, track)
	bar := player.HookSnapTransitionToBar(1000, 0, track)
	periodFrames := track.PeriodSec * dsp.FramesPerSecond
	firstBeatFrame := track.FirstBeat * dsp.FramesPerSecond

	gridBeat := (float64(grid) - firstBeatFrame) / periodFrames
	if math.Abs(gridBeat-math.Round(gridBeat)) >= 0.02 {
		t.Errorf("beat snap %d lands on beat %.3f, want a whole beat", grid, gridBeat)
	}

	barBeat := (float64(bar) - firstBeatFrame) / periodFrames
	if math.Abs(barBeat-math.Round(barBeat)) >= 0.02 {
		t.Errorf("bar snap %d lands on beat %.3f, want a whole beat", bar, barBeat)
	}

	barPhase := math.Mod(math.Round(barBeat)-float64(track.DownbeatPhase), analysis.BarBeats)
	if barPhase < 0 {
		barPhase += analysis.BarBeats
	}
	if barPhase != 0 {
		t.Errorf("bar snap %d sits at bar phase %.0f, want 0", bar, barPhase)
	}
}

func TestBarSnapIsWithinOneBarOfBeatSnap(t *testing.T) {
	track := audiotest.AnalyzeAccentedClickTrack(t)

	grid := player.HookSnapTransitionToGrid(1000, 0, track)
	bar := player.HookSnapTransitionToBar(1000, 0, track)
	barFrames := track.PeriodSec * dsp.FramesPerSecond * analysis.BarBeats

	if distance := math.Abs(float64(bar - grid)); distance > barFrames {
		t.Errorf("bar snap %d is %.1f frames from beat snap %d, want at most one bar (%.1f frames)", bar, distance, grid, barFrames)
	}
}
