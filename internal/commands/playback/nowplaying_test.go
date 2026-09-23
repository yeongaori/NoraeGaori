package playback

import (
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/queuetest"
)

func liveSong(title string) *queue.Song {
	song := queuetest.Song(title, commandtest.CallerID)
	song.IsLive = true
	return song
}

func TestNowPlaying(t *testing.T) {
	nextSong := messages.T(commandtest.GuildID).Fields.NextSong

	commandtest.Run(t, "nowplaying", HandleNowPlaying, []commandtest.Case{
		{
			Name:     "an empty queue",
			WantText: func(locale *messages.Locale) string { return locale.Errors.EmptyQueue },
		},
		{
			Name:     "an idle queue",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 2),
			WantText: func(locale *messages.Locale) string { return locale.Music.NowPlayingPaused },
			Check:    commandtest.ReplyContains(nextSong, "Song 2"),
		},
		{
			Name:     "a playing song",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 1),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Music.NowPlayingPlaying },
			Check:    commandtest.ReplyContains("0:00 / 3:00"),
		},
		{
			Name:     "a loading song",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 1),
			Prepare:  commandtest.Loading,
			WantText: func(locale *messages.Locale) string { return locale.Music.NowPlayingLoading },
		},
		{
			Name:     "a single live song",
			Songs:    []*queue.Song{liveSong("Live")},
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Music.NowPlayingPlaying },
			Check:    commandtest.ReplyLacks(" / ", nextSong),
		},
	})
}
