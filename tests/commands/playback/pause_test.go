package playback_test

import (
	"testing"

	"noraegaori/internal/commands/playback"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/queuetest"
)

func loadingWithAutoLeave(isEnabled bool) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		if err := queue.SetAutoLeave(commandtest.GuildID, isEnabled); err != nil {
			t.Fatalf("failed to set auto-leave: %v", err)
		}
		commandtest.Loading(t)
		player.GetPlayer(commandtest.GuildID).Loading = true
		t.Cleanup(func() { player.DeletePlayer(commandtest.GuildID) })
	}
}

func TestPauseReplyFollowsAutoLeave(t *testing.T) {
	commandtest.Run(t, "pause", playback.HandlePause, []commandtest.Case{
		{
			Name:       "auto-leave on",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    loadingWithAutoLeave(true),
			WantText:   func(locale *messages.Locale) string { return locale.Descriptions.Paused },
		},
		{
			Name:       "auto-leave off",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    loadingWithAutoLeave(false),
			WantText:   func(locale *messages.Locale) string { return locale.Descriptions.PausedStay },
		},
	})
}
