package player

import (
	"fmt"
	"math"
	"noraegaori/internal/audio/ffmpeg"
	"reflect"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/dsp"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/queue"
)

func checkAnnouncementGate(c *checkCollector) {
	guildID := "check-announce-guild"
	clearAnnounced(guildID)
	defer clearAnnounced(guildID)

	first := markAnnounced(guildID, 101)
	repeat := markAnnounced(guildID, 101)
	c.add("first announcement for a song is allowed", first, "got %v", first)
	c.add("repeat announcement for the same song is suppressed", !repeat, "got %v", repeat)

	different := markAnnounced(guildID, 102)
	c.add("a different song is announced", different, "got %v", different)

	back := markAnnounced(guildID, 101)
	c.add("returning to an earlier song announces again", back, "got %v", back)

	clearAnnounced(guildID)
	afterClear := markAnnounced(guildID, 101)
	c.add("clearing re-arms the same song for repeat playback", afterClear, "got %v", afterClear)

	otherGuild := "check-announce-guild-other"
	clearAnnounced(otherGuild)
	defer clearAnnounced(otherGuild)
	c.add("announcement state is per guild", markAnnounced(otherGuild, 101), "expected an independent guild to announce")

	crossfadeAdvance := markAnnounced(guildID, 200)
	retryAfterFailure := markAnnounced(guildID, 200)
	seekRestart := markAnnounced(guildID, 200)
	total := 0
	for _, announced := range []bool{crossfadeAdvance, retryAfterFailure, seekRestart} {
		if announced {
			total++
		}
	}
	c.add("crossfade then retry then seek announces exactly once", total == 1, "announced %d times", total)

	clearAnnounced(guildID)
	failed := markAnnounced(guildID, 300)
	clearAnnounced(guildID)
	c.add("a removed song leaves no announcement state", failed && !containsAnnouncement(guildID), "announced %v, state remains %v", failed, containsAnnouncement(guildID))
}

func containsAnnouncement(guildID string) bool {
	announcedSongsMu.Lock()
	defer announcedSongsMu.Unlock()
	_, exists := announcedSongs[guildID]
	return exists
}

func checkRestartStreamURL(c *checkCollector) {
	guildID := "check-restart-guild"
	song := &queue.Song{ID: 1, URL: "https://example.invalid/watch?v=check"}

	kept, err := resolveRestartStreamURL(guildID, song, false, 96000, "https://cdn.invalid/existing")
	c.add("a populated stream URL is reused on restart", kept == "https://cdn.invalid/existing" && err == nil,
		"got %q err=%v", kept, err)

	live := &queue.Song{ID: 2, URL: "https://example.invalid/watch?v=live", IsLive: true}
	liveURL, liveErr := resolveRestartStreamURL(guildID, live, false, 96000, "")
	c.add("a live song restarts without a stream URL", liveURL == "" && liveErr == nil,
		"got %q err=%v", liveURL, liveErr)
}

func checkTransitionSlide(c *checkCollector) {
	cs := newCrossfadeState()
	cs.armed = true
	cs.transitionFrame = 1000
	cs.crossfadeFrames = 400
	cs.minUsableFrames = 100
	cs.totalFrames = 1600
	cs.slideFrames = 50

	cs.slideTransition("check")
	c.add("sliding pushes the transition forward by one beat", cs.transitionFrame == 1050,
		"transitionFrame %d", cs.transitionFrame)
	c.add("sliding leaves the crossfade intact while the window still fits", cs.crossfadeFrames == 400,
		"crossfadeFrames %d with %d frames remaining", cs.crossfadeFrames, cs.totalFrames-cs.transitionFrame)

	cs.transitionFrame = 1250
	cs.slideTransition("check")
	c.add("sliding shrinks the crossfade once it overruns the window", cs.crossfadeFrames == 300,
		"crossfadeFrames %d at frame %d of %d", cs.crossfadeFrames, cs.transitionFrame, cs.totalFrames)

	previous := cs.crossfadeFrames
	for i := 0; i < 40 && !cs.cancelled; i++ {
		cs.slideTransition("check")
		if cs.cancelled {
			break
		}
		if cs.crossfadeFrames > previous {
			c.add("crossfade window never grows while sliding", false, "grew from %d to %d", previous, cs.crossfadeFrames)
			return
		}
		previous = cs.crossfadeFrames
	}
	c.add("crossfade window never grows while sliding", true, "final %d frames", previous)
	c.add("sliding eventually cancels once the window is unusable", cs.cancelled && !cs.armed,
		"cancelled %v armed %v", cs.cancelled, cs.armed)

	fresh := newCrossfadeState()
	c.add("a new crossfade has no refetch parked", fresh.bRefetch.Load() == nil && !fresh.bRefetching.Load() && !fresh.bRetried,
		"refetch %v refetching %v retried %v", fresh.bRefetch.Load(), fresh.bRefetching.Load(), fresh.bRetried)

	fresh.abort()
	c.add("aborting marks the refetch as unwanted", fresh.bAborted.Load(), "bAborted %v", fresh.bAborted.Load())
}

func checkAnalysisReadCap(c *checkCollector) {
	requested := int64(analysisHeadSecs * analysis.SampleRate * 4)
	c.add("the analysis read cap covers the full requested head",
		analysisMaxBytes > requested,
		"cap %d bytes vs requested %d bytes", analysisMaxBytes, requested)

	margin := analysisMaxBytes - requested
	c.add("the analysis read cap keeps a bounded margin",
		margin > 0 && margin < requested,
		"margin %d bytes", margin)

	minimumSamples := int(analysis.MinSeconds * analysis.SampleRate)
	c.add("the analysis read cap admits far more than the minimum analysable length",
		analysisMaxBytes/4 > int64(minimumSamples),
		"cap %d samples vs minimum %d samples", analysisMaxBytes/4, minimumSamples)
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

func checkStyleResolution(c *checkCollector) {
	analysisA := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240, Tonic: 9, Minor: true, KeyConfidence: 0.5}
	analysisB := &analysis.TrackAnalysis{BPM: 127, PeriodSec: 60.0 / 127, Duration: 240, Tonic: 4, Minor: true, KeyConfidence: 0.5}

	categories := []string{"volume", "eq", "filter", "effect", "loop"}

	autoStyles := transition.AutoStyles(analysisA, analysisB)
	validAuto := true
	for _, category := range categories {
		if !transition.ValidStyle(category, autoStyles[category]) {
			validAuto = false
		}
	}
	c.add("auto styles are valid style keys", validAuto, "auto=%v", autoStyles)

	layerFailures := 0
	for _, category := range categories {
		values := transition.StyleValues(category)
		guildStyle := values[len(values)-1]
		songStyle := values[1]

		_, effective, source := transition.ResolveStyles(analysisA, analysisB, true, transition.StyleOverrides{}, transition.StyleOverrides{})
		if effective[category] != autoStyles[category] || source[category] != "auto" {
			layerFailures++
		}

		_, effective, source = transition.ResolveStyles(analysisA, analysisB, true, styleOverridesFor(category, guildStyle), transition.StyleOverrides{})
		if effective[category] != guildStyle || source[category] != "guild" {
			layerFailures++
		}

		_, effective, source = transition.ResolveStyles(analysisA, analysisB, true,
			styleOverridesFor(category, guildStyle), styleOverridesFor(category, songStyle))
		if effective[category] != songStyle || source[category] != "song" {
			layerFailures++
		}

		_, effective, source = transition.ResolveStyles(analysisA, analysisB, true,
			styleOverridesFor(category, guildStyle), styleOverridesFor(category, transition.StyleAuto))
		if effective[category] != guildStyle || source[category] != "guild" {
			layerFailures++
		}

		_, effective, source = transition.ResolveStyles(analysisA, analysisB, true,
			styleOverridesFor(category, "not_a_real_style"), styleOverridesFor(category, ""))
		if effective[category] != autoStyles[category] || source[category] != "auto" {
			layerFailures++
		}
	}
	c.add("style precedence song over guild over auto", layerFailures == 0,
		"%d failures across %d categories", layerFailures, len(categories))

	crossTalk := 0
	_, effective, source := transition.ResolveStyles(analysisA, analysisB, true,
		transition.StyleOverrides{}, transition.StyleOverrides{Effect: "echo_half_cut_end"})
	for _, category := range categories {
		if category == "effect" {
			continue
		}
		if effective[category] != autoStyles[category] || source[category] != "auto" {
			crossTalk++
		}
	}
	c.add("override affects only its own category", crossTalk == 0 && effective["effect"] == "echo_half_cut_end",
		"effect=%s crosstalk=%d", effective["effect"], crossTalk)

	_, nilEffective, nilSource := transition.ResolveStyles(nil, nil, true, transition.StyleOverrides{}, transition.StyleOverrides{})
	nilDefaults := nilEffective["volume"] == "smooth" && nilEffective["eq"] == "none" &&
		nilEffective["filter"] == "none" && nilEffective["effect"] == "none" &&
		nilEffective["loop"] == "none" && nilSource["volume"] == "auto"
	c.add("nil analysis resolves to the default recipe", nilDefaults, "effective=%v", nilEffective)

	defaultStyles := transition.RecipeStyleMap(transition.DefaultRecipe())
	_, offEffective, offSource := transition.ResolveStyles(analysisA, analysisB, false, transition.StyleOverrides{}, transition.StyleOverrides{})
	autoSkipped := true
	for _, category := range categories {
		if offEffective[category] != defaultStyles[category] || offSource[category] != "auto" {
			autoSkipped = false
		}
	}
	c.add("auto selection is skipped when disabled", autoSkipped && !reflect.DeepEqual(offEffective, autoStyles),
		"off=%v auto=%v", offEffective, autoStyles)

	_, offOverridden, offOverriddenSource := transition.ResolveStyles(analysisA, analysisB, false,
		transition.StyleOverrides{EQ: "quick_bass"}, transition.StyleOverrides{Effect: "reverb_out_end"})
	offLayers := offOverridden["eq"] == "quick_bass" && offOverriddenSource["eq"] == "guild" &&
		offOverridden["effect"] == "reverb_out_end" && offOverriddenSource["effect"] == "song" &&
		offOverridden["volume"] == defaultStyles["volume"] && offOverriddenSource["volume"] == "auto"
	c.add("overrides still layer when auto selection is disabled", offLayers, "effective=%v", offOverridden)
}

func checkOutroResolution(c *checkCollector) {
	categories := []string{"volume", "eq", "filter", "effect", "loop"}

	inputs := []struct {
		name  string
		track *analysis.TrackAnalysis
	}{
		{"nil analysis", nil},
		{"zero bpm", &analysis.TrackAnalysis{BPM: 0, PeriodSec: 0}},
		{"bpm without grid", &analysis.TrackAnalysis{BPM: 128, PeriodSec: 0}},
		{"full grid", &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Tonic: 9, Minor: true, KeyConfidence: 0.5}},
	}

	invalid := 0
	silent := 0
	for _, input := range inputs {
		styles := transition.AutoOutroStyles(input.track)
		for _, category := range categories {
			if !transition.ValidStyle(category, styles[category]) {
				invalid++
			}
		}
		if styles["effect"] == "none" && styles["filter"] == "none" {
			silent++
		}
	}
	c.add("outro recipes are valid across every analysis shape", invalid == 0, "%d invalid style keys", invalid)
	c.add("every outro shapes the ending", silent == 0, "%d inputs produced no filter and no effect", silent)

	gridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}
	ungridded := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 0}
	c.add("outro differs from the ordinary transition recipe",
		!reflect.DeepEqual(transition.AutoOutroStyles(gridded), transition.AutoStyles(gridded, gridded)),
		"outro=%v transition=%v", transition.AutoOutroStyles(gridded), transition.AutoStyles(gridded, gridded))
	c.add("a beat grid earns a rhythmic outro",
		transition.AutoOutroStyles(gridded)["effect"] == "echo_half_cut_end" &&
			transition.AutoOutroStyles(ungridded)["effect"] != "echo_half_cut_end",
		"grid=%s no-grid=%s", transition.AutoOutroStyles(gridded)["effect"], transition.AutoOutroStyles(ungridded)["effect"])

	defaultStyles := transition.RecipeStyleMap(transition.DefaultRecipe())
	_, offEffective, offSource := transition.ResolveOutroStyles(gridded, false, transition.StyleOverrides{}, transition.StyleOverrides{})
	offSkipped := true
	for _, category := range categories {
		if offEffective[category] != defaultStyles[category] || offSource[category] != "auto" {
			offSkipped = false
		}
	}
	c.add("outro auto selection is skipped when disabled", offSkipped, "off=%v", offEffective)

	_, layered, layeredSource := transition.ResolveOutroStyles(gridded, true,
		transition.StyleOverrides{Volume: "fadein_cutout"}, transition.StyleOverrides{Effect: "reverb_out_center"})
	outroLayers := layered["volume"] == "fadein_cutout" && layeredSource["volume"] == "guild" &&
		layered["effect"] == "reverb_out_center" && layeredSource["effect"] == "song" &&
		layered["filter"] == transition.AutoOutroStyles(gridded)["filter"] && layeredSource["filter"] == "auto"
	c.add("outro overrides layer song over guild over auto", outroLayers, "effective=%v", layered)

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
	c.add("every volume style gives the outro a distinct shape", len(distinct) == 5,
		"%d distinct A-side curves across 5 styles", len(distinct))

	checkOutroWindow(c)
}

func checkOutroWindow(c *checkCollector) {
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}
	fade := fadeSettings{autoMix: true, autoMixBeats: 16, crossfadeSec: 8}
	expectedFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	full := &ffmpeg.EndState{TotalFrames: 12000, Analysis: track}
	start, frames, ok := planOutroWindow(full, 100, fade)
	c.add("outro window sits at the end of the track",
		ok && frames == expectedFrames && start == full.TotalFrames-expectedFrames && start > 100,
		"start=%d frames=%d expected=%d ok=%v", start, frames, expectedFrames, ok)

	trimmed := &ffmpeg.EndState{TotalFrames: 12000, SilentTailFrames: 500, Analysis: track}
	trimStart, _, trimOK := planOutroWindow(trimmed, 100, fade)
	c.add("outro window respects the trimmed silent tail",
		trimOK && trimStart == 12000-500-expectedFrames,
		"start=%d expected=%d", trimStart, 12000-500-expectedFrames)

	_, _, lateOK := planOutroWindow(full, full.TotalFrames-expectedFrames, fade)
	c.add("outro refuses a window that starts in the past", !lateOK, "ok=%v", lateOK)

	short := &ffmpeg.EndState{TotalFrames: 200, Analysis: track}
	_, _, shortOK := planOutroWindow(short, 100, fade)
	c.add("outro refuses a track too short to hold it", !shortOK, "ok=%v", shortOK)

	_, _, offOK := planOutroWindow(full, 100, fadeSettings{autoMix: false, autoMixBeats: 16, crossfadeSec: 8})
	noGridStart, noGridFrames, noGridOK := planOutroWindow(
		&ffmpeg.EndState{TotalFrames: 12000}, 100, fade)
	c.add("outro window still resolves without analysis",
		offOK && noGridOK && noGridFrames == int(transition.FallbackCrossfadeSec*dsp.FramesPerSecond) && noGridStart > 100,
		"start=%d frames=%d", noGridStart, noGridFrames)
}

func checkTransitionTiming(c *checkCollector) {
	track := &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128, Duration: 240}

	frames, seconds := transition.CrossfadeFrames(false, 16, 8, track)
	c.add("crossfade frames follow the configured duration when automix is off",
		frames == int(8*dsp.FramesPerSecond) && seconds == 8, "frames=%d seconds=%.2f", frames, seconds)

	frames, seconds = transition.CrossfadeFrames(true, 16, 8, track)
	beatDerived := float64(16) * track.PeriodSec
	c.add("crossfade frames follow the beat grid when automix is on",
		seconds > beatDerived-0.001 && seconds < beatDerived+0.001 && frames == int(seconds*dsp.FramesPerSecond),
		"frames=%d seconds=%.3f expected=%.3f", frames, seconds, beatDerived)

	frames, _ = transition.CrossfadeFrames(true, 16, 0, nil)
	c.add("crossfade frames fall back without analysis", frames == int(transition.FallbackCrossfadeSec*dsp.FramesPerSecond),
		"frames=%d", frames)

	_, seconds = transition.CrossfadeFrames(true, 4096, 8, track)
	c.add("crossfade seconds are clamped to the maximum", seconds == transition.CrossfadeMaxSec, "seconds=%.2f", seconds)

	crossfadeFrames, _ := transition.CrossfadeFrames(true, 16, 8, track)

	style, loopFrames := transition.ClampLoopStyle(transition.LoopFourBeats, track.PeriodSec, crossfadeFrames)
	c.add("loop survives when it fits inside the crossfade", style == transition.LoopFourBeats && loopFrames > 0,
		"style=%s frames=%d of %d", style, loopFrames, crossfadeFrames)

	style, loopFrames = transition.ClampLoopStyle(transition.LoopEightBeats, track.PeriodSec, crossfadeFrames)
	c.add("loop is dropped when it needs more than half the crossfade", style == transition.LoopNone && loopFrames == 0,
		"style=%s frames=%d of %d", style, loopFrames, crossfadeFrames)

	style, loopFrames = transition.ClampLoopStyle(transition.LoopFourBeats, 0, crossfadeFrames)
	c.add("loop is dropped without a beat grid", style == transition.LoopNone && loopFrames == 0,
		"style=%s frames=%d", style, loopFrames)

	style, loopFrames = transition.ClampLoopStyle(transition.LoopNone, track.PeriodSec, crossfadeFrames)
	c.add("loop none stays none", style == transition.LoopNone && loopFrames == 0, "style=%s frames=%d", style, loopFrames)
}

func checkAnalysisHelpers(c *checkCollector) {
	bpm, key, camelot, hasKey := analysis.Summarize(nil)
	c.add("analysis summary handles nil", bpm == 0 && key == "" && camelot == "" && !hasKey,
		"bpm=%.1f key=%q camelot=%q hasKey=%v", bpm, key, camelot, hasKey)

	lowConfidence := &analysis.TrackAnalysis{BPM: 120, Tonic: 3, Minor: false, KeyConfidence: analysis.KeyConfidenceFloor / 2}
	bpm, _, _, hasKey = analysis.Summarize(lowConfidence)
	c.add("analysis summary hides low confidence keys", bpm == 120 && !hasKey,
		"bpm=%.1f hasKey=%v confidence=%.4f", bpm, hasKey, lowConfidence.KeyConfidence)

	confident := &analysis.TrackAnalysis{BPM: 174, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	_, key, camelot, hasKey = analysis.Summarize(confident)
	c.add("analysis summary reports confident keys", hasKey && key == "C major" && camelot == "8B",
		"key=%q camelot=%q", key, camelot)

	if _, distance, ok := analysis.Compare(nil, confident); ok || distance != -1 {
		c.add("compatibility rejects nil input", false, "ok=%v distance=%d", ok, distance)
	} else {
		c.add("compatibility rejects nil input", true, "ok=false distance=-1")
	}

	zeroBPM := &analysis.TrackAnalysis{BPM: 0, KeyConfidence: 0.5}
	_, _, ok := analysis.Compare(zeroBPM, confident)
	c.add("compatibility rejects zero BPM", !ok, "ok=%v", ok)

	slower := &analysis.TrackAnalysis{BPM: 120, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	faster := &analysis.TrackAnalysis{BPM: 132, Tonic: 0, Minor: false, KeyConfidence: 0.5}
	delta, distance, ok := analysis.Compare(slower, faster)
	c.add("compatibility reports signed BPM delta", ok && math.Abs(delta-0.1) < 1e-9 && distance == 0,
		"delta=%.4f distance=%d", delta, distance)
}
