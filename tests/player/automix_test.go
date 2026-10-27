package player_test

import (
	"math"
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/player"
	"noraegaori/tests/testutil/audiotest"
)

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

func outroWindowFixture() (*ffmpeg.EndState, *player.HookFadeSettings, int) {
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
	fade := player.HookBuildFadeSettings(&player.HookFadeSettingsFields{AutoMix: true, AutoMixBeats: 16, CrossfadeSec: 8})
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

	if _, _, ok := player.HookPlanOutroWindow(full, 100, player.HookBuildFadeSettings(&player.HookFadeSettingsFields{AutoMix: false, AutoMixBeats: 16, CrossfadeSec: 8})); !ok {
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
	bpm, key, code, hasKey := analysis.Summarize(nil)

	if bpm != 0 {
		t.Errorf("bpm = %.1f, want 0", bpm)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
	if code != "" {
		t.Errorf("code = %q, want empty", code)
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
	_, key, code, hasKey := analysis.Summarize(&analysis.TrackAnalysis{BPM: 174, Tonic: 0, Minor: false, KeyConfidence: 0.5})

	if !hasKey {
		t.Fatal("hasKey is false for a confident key")
	}
	if key != "C major" {
		t.Errorf("key = %q, want C major", key)
	}
	if code != "8B" {
		t.Errorf("code = %q, want 8B", code)
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
