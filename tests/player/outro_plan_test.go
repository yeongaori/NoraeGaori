package player_test

import (
	"testing"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

func outroEndState() *ffmpeg.EndState {
	return &ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 4500, Analysis: &analysis.TrackAnalysis{BPM: 128, PeriodSec: 60.0 / 128}}
}

func outroFade(overrides map[string]string) *player.HookFadeSettings {
	return player.HookBuildFadeSettings(player.HookFadeSettingsFields{
		AutoMix: true, Crossfade: true, AutoMixBeats: 64, RepeatMode: queue.RepeatOff, StyleOverrides: overrides,
	})
}

func TestLastSongEndsNaturallyWithoutOverrides(t *testing.T) {
	guildID := "outronatural"
	setupPlayerDB(t, guildID, 1)

	outro := player.HookNewOutroState()
	if outro.HookPlan(player.GetPlayer(guildID), outroEndState(), 100, outroFade(nil)) {
		t.Error("an outro was planned for the last song, want it to play to its natural end")
	}
	if *outro.HookProcessor() != nil {
		t.Error("an unplanned outro still built a processor")
	}
}

func TestIncomingChoicesLeaveTheLastSongAlone(t *testing.T) {
	guildID := "outroincoming"
	setupPlayerDB(t, guildID, 1)

	outro := player.HookNewOutroState()
	overrides := map[string]string{"volume_in": "crossfade", "fx_in": "phaser", "eq_in": "hi_fast"}
	if outro.HookPlan(player.GetPlayer(guildID), outroEndState(), 100, outroFade(overrides)) {
		t.Error("an outro was planned from incoming-side choices, want the last song to end naturally")
	}
}

func TestOutroOverrideShapesTheLastSong(t *testing.T) {
	guildID := "outrooverride"
	setupPlayerDB(t, guildID, 1)

	outro := player.HookNewOutroState()
	fade := outroFade(transition.ExpandLegacy(transition.ShortcutVolume, "crossfade"))
	if !outro.HookPlan(player.GetPlayer(guildID), outroEndState(), 100, fade) {
		t.Fatal("no outro was planned for a guild that set a volume style")
	}
	processor := *outro.HookProcessor()
	if processor == nil {
		t.Fatal("the planned outro has no processor")
	}
	if start, _ := processor.Gains(0); start != 1 {
		t.Errorf("outro starts at %.3f, want the song at full level", start)
	}
	if middle, _ := processor.Gains(0.5); middle < 0.45 || middle > 0.55 {
		t.Errorf("outro at its middle is %.3f, want the linear fade halfway down", middle)
	}
}

func TestOutroIsLeftToTheCrossfadeWhenAnotherSongFollows(t *testing.T) {
	guildID := "outronext"
	setupPlayerDB(t, guildID, 2)

	outro := player.HookNewOutroState()
	if outro.HookPlan(player.GetPlayer(guildID), outroEndState(), 100, outroFade(transition.CategoryVolumeOut.Override("crossfade"))) {
		t.Error("an outro was planned although another song follows")
	}
}
