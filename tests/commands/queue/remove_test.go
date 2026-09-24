package queue_test

import (
	"fmt"
	"testing"

	queuecommand "noraegaori/internal/commands/queue"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestRemove(t *testing.T) {
	caller, other := commandtest.CallerID, commandtest.OtherID
	inVoice := []string{caller}
	invalidRange := func(locale *messages.Locale) string { return locale.Queue.InvalidRange }
	songRemoved := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SongRemoved, "Song 2") }
	rangeRemoved := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.RangeRemoved, 2, 2, 3) }

	commandtest.Run(t, "remove", queuecommand.HandleRemove, []commandtest.Case{
		{
			Name:     "a missing position",
			WantText: func(locale *messages.Locale) string { return locale.Queue.EnterPosition },
		},
		{
			Name:     "an empty queue",
			Options:  removeOption("1"),
			WantText: emptyQueueText,
		},
		{
			Name:     "all with none of the caller's songs",
			Songs:    queuetest.Songs(other, other),
			Options:  removeOption("all"),
			WantText: func(locale *messages.Locale) string { return locale.Queue.NoUserSongs },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "all with only the playing song",
			Songs:    queuetest.Songs(caller, other),
			Options:  removeOption("ALL"),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Queue.OnlyCurrentSong },
		},
		{
			Name:     "all of the caller's songs",
			Songs:    queuetest.Songs(caller, other, caller),
			Options:  removeOption("all"),
			WantText: func(*messages.Locale) string { return "2 songs by caller have been removed." },
			Check:    commandtest.WantTitles("Song 2"),
		},
		{
			Name:     "all of the caller's songs while one plays",
			Songs:    queuetest.Songs(caller, other, caller),
			Options:  removeOption("all"),
			Prepare:  commandtest.Playing,
			WantText: func(*messages.Locale) string { return "1 songs by caller have been removed." },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "a range with too many parts",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("1-2-3"),
			WantText: invalidRange,
		},
		{
			Name:     "a range that is not a number",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("a-b"),
			WantText: invalidRange,
		},
		{
			Name:     "a backwards range",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("3-1"),
			WantText: invalidRange,
		},
		{
			Name:     "a range from zero",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("0-2"),
			WantText: invalidRange,
			Check:    commandtest.WantTitles("Song 1", "Song 2", "Song 3"),
		},
		{
			Name:     "a range starting past the end",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("4-5"),
			WantText: invalidRange,
			Check:    commandtest.WantTitles("Song 1", "Song 2", "Song 3"),
		},
		{
			Name:     "a spaced range from the idle head",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption(" 1 - 2 "),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.RangeRemoved, 2, 1, 2) },
			Check:    commandtest.WantTitles("Song 3"),
		},
		{
			Name:     "a range over the playing song",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("1-2"),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Queue.RangeIncludesCurrent },
			Check:    commandtest.WantTitles("Song 1", "Song 2", "Song 3"),
		},
		{
			Name:     "an owned range past the end",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("2-9"),
			WantText: rangeRemoved,
			Check:    commandtest.WantTitles("Song 1"),
		},
		{
			Name:       "a range with another user's song",
			Songs:      queuetest.Songs(caller, other, caller),
			VoiceUsers: inVoice,
			Options:    removeOption("2-3"),
			WantText:   rangeRemoved,
			Check:      commandtest.WantTitles("Song 1"),
		},
		{
			Name:     "a position that is not a number",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  removeOption("x"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 2) },
		},
		{
			Name:     "position zero",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  removeOption("0"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 2) },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "the position just past the queue",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  removeOption("3"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 2) },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "the last song",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("3"),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SongRemoved, "Song 3") },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "the playing song",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  removeOption("1"),
			Prepare:  commandtest.Loading,
			WantText: func(locale *messages.Locale) string { return locale.Queue.CannotRemoveCurrent },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "an owned song",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  removeOption("2"),
			WantText: songRemoved,
			Check:    commandtest.WantTitles("Song 1"),
		},
		{
			Name:     "another user's song with the caller outside voice",
			Songs:    queuetest.Songs(caller, other),
			Options:  removeOption("2"),
			WantText: func(locale *messages.Locale) string { return locale.Music.EnterVoiceChannel },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:       "another user's song with the caller alone in voice",
			Songs:      queuetest.Songs(caller, other),
			VoiceUsers: inVoice,
			Options:    removeOption("2"),
			WantText:   songRemoved,
			Check:      commandtest.WantTitles("Song 1"),
		},
	})
}
