package queue

import (
	"fmt"
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/queuetest"
)

func TestMoveTrack(t *testing.T) {
	caller := commandtest.CallerID
	outOfRange := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Admin.EnterValidRange, 3) }
	cannotMovePlaying := func(locale *messages.Locale) string { return locale.Admin.CannotMovePlaying }
	unchanged := commandtest.WantTitles("Song 1", "Song 2", "Song 3")

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
			Name:     "position zero",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  moveOptions(0, 2),
			WantText: outOfRange,
			Check:    unchanged,
		},
		{
			Name:     "the position just past the queue",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  moveOptions(2, 4),
			WantText: outOfRange,
			Check:    unchanged,
		},
		{
			Name:     "the same position",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  moveOptions(2, 2),
			WantText: func(locale *messages.Locale) string { return locale.Admin.SamePosition },
			Check:    unchanged,
		},
		{
			Name:     "the playing song moved away",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  moveOptions(1, 3),
			Prepare:  commandtest.Playing,
			WantText: cannotMovePlaying,
			Check:    unchanged,
		},
		{
			Name:     "a song moved in front of the loading song",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  moveOptions(3, 1),
			Prepare:  commandtest.Loading,
			WantText: cannotMovePlaying,
			Check:    unchanged,
		},
		{
			Name:    "a move behind the playing song",
			Songs:   queuetest.SongsBy(caller, 3),
			Options: moveOptions(3, 2),
			Prepare: commandtest.Playing,
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Admin.MoveCompleteDesc, "Song 3", 3, 2)
			},
			Check: commandtest.WantTitles("Song 1", "Song 3", "Song 2"),
		},
		{
			Name:    "a move to the front while idle",
			Songs:   queuetest.SongsBy(caller, 3),
			Options: moveOptions(3, 1),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Admin.MoveCompleteDesc, "Song 3", 3, 1)
			},
			Check: commandtest.WantTitles("Song 3", "Song 1", "Song 2"),
		},
	})
}
