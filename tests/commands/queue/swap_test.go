package queue_test

import (
	"fmt"
	"testing"

	queuecommand "noraegaori/internal/commands/queue"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestSwap(t *testing.T) {
	caller, other := commandtest.CallerID, commandtest.OtherID
	outOfRange := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 2) }
	unchanged := commandtest.WantTitles("Song 1", "Song 2", "Song 3")

	commandtest.Run(t, "swap", queuecommand.HandleSwap, []commandtest.Case{
		{
			Name:     "missing positions",
			WantText: func(locale *messages.Locale) string { return locale.Queue.SwapEnterPositions },
		},
		{
			Name:     "an empty queue",
			Options:  swapOptions(1, 2),
			WantText: emptyQueueText,
		},
		{
			Name:     "position zero",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  swapOptions(0, 2),
			WantText: outOfRange,
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "the position just past the queue",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  swapOptions(1, 3),
			WantText: outOfRange,
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "the loading song",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  swapOptions(1, 2),
			Prepare:  commandtest.Loading,
			WantText: func(locale *messages.Locale) string { return locale.Queue.CannotSwapCurrent },
			Check:    unchanged,
		},
		{
			Name:     "the playing song in the second position",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  swapOptions(3, 1),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Queue.CannotSwapCurrent },
			Check:    unchanged,
		},
		{
			Name:     "another user's song second",
			Songs:    queuetest.Songs(caller, other, caller),
			Options:  swapOptions(1, 2),
			WantText: func(locale *messages.Locale) string { return locale.Queue.OnlyOwnSwap },
			Check:    unchanged,
		},
		{
			Name:     "another user's song first",
			Songs:    queuetest.Songs(caller, other, caller),
			Options:  swapOptions(2, 3),
			WantText: func(locale *messages.Locale) string { return locale.Queue.OnlyOwnSwap },
			Check:    unchanged,
		},
		{
			Name:    "a completed swap behind the playing song",
			Songs:   queuetest.SongsBy(caller, 3),
			Options: swapOptions(3, 2),
			Prepare: commandtest.Playing,
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Queue.SwapCompleteDesc, 3, "Song 3", 2, "Song 2")
			},
			Check: commandtest.WantTitles("Song 1", "Song 3", "Song 2"),
		},
		{
			Name:    "a completed swap while idle",
			Songs:   queuetest.SongsBy(caller, 3),
			Options: swapOptions(1, 3),
			WantText: func(locale *messages.Locale) string {
				return fmt.Sprintf(locale.Queue.SwapCompleteDesc, 1, "Song 1", 3, "Song 3")
			},
			Check: commandtest.WantTitles("Song 3", "Song 2", "Song 1"),
		},
	})
}
