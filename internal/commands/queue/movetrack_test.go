package queue

import (
	"fmt"
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/queuetest"
)

func TestMoveTrack(t *testing.T) {
	commandtest.Run(t, "movetrack", HandleMoveTrack, []commandtest.Case{
		{
			Name:     "missing positions",
			WantText: func(locale *messages.Locale) string { return locale.Admin.EnterPositions },
		},
		{
			Name:     "an empty queue",
			Options:  moveOptions(1, 2),
			WantText: emptyQueueText,
		},
		{
			Name:     "a position past the queue",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 2),
			Options:  moveOptions(1, 5),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Admin.EnterValidRange, 2) },
		},
		{
			Name:     "the same position",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 2),
			Options:  moveOptions(2, 2),
			WantText: func(locale *messages.Locale) string { return locale.Admin.SamePosition },
		},
		{
			Name:     "the playing song",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 3),
			Options:  moveOptions(1, 3),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Admin.CannotMovePlaying },
			Check:    commandtest.WantTitles("Song 1", "Song 2", "Song 3"),
		},
		{
			Name:     "a completed move",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 3),
			Options:  moveOptions(3, 1),
			WantText: func(locale *messages.Locale) string { return locale.Admin.MoveCompleteTitle },
			Check:    commandtest.WantTitles("Song 3", "Song 1", "Song 2"),
		},
	})
}
