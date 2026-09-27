package playback_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/playback"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func reconciledAfterLoading(t *testing.T) {
	t.Helper()

	commandtest.Loading(t)
	player.ReconcileState(commandtest.GuildID)
}

func TestPlaybackCommandsWithoutAPlayingSession(t *testing.T) {
	notPlaying := func(locale *messages.Locale) string { return locale.Music.NotPlayingOrLoading }

	commandtest.Run(t, "pause", playback.HandlePause, []commandtest.Case{
		{
			Name:       "the stored queue still claims loading",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    commandtest.Loading,
			WantText:   notPlaying,
		},
	})

	commandtest.Run(t, "seek", playback.HandleSeek, []commandtest.Case{
		{
			Name:       "the song is paused",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Options:    []*discordgo.ApplicationCommandInteractionDataOption{discordtest.StringOption("position", "0:30")},
			WantText:   notPlaying,
		},
	})

	commandtest.Run(t, "nowplaying", playback.HandleNowPlaying, []commandtest.Case{
		{
			Name:     "a stale loading flag reconciled before the command",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 1),
			Prepare:  reconciledAfterLoading,
			WantText: func(locale *messages.Locale) string { return locale.Music.NowPlayingPaused },
		},
	})
}
