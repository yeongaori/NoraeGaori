package queue

import (
	"fmt"
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/queuetest"
)

func TestSwap(t *testing.T) {
	commandtest.Run(t, "swap", HandleSwap, []commandtest.Case{
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
			Name:     "a position past the queue",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 2),
			Options:  swapOptions(1, 5),
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 2) },
		},
		{
			Name:     "the loading song",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 2),
			Options:  swapOptions(1, 2),
			Prepare:  commandtest.Loading,
			WantText: func(locale *messages.Locale) string { return locale.Queue.CannotSwapCurrent },
		},
		{
			Name:     "another user's song",
			Songs:    queuetest.Songs(commandtest.CallerID, commandtest.OtherID),
			Options:  swapOptions(1, 2),
			WantText: func(locale *messages.Locale) string { return locale.Queue.OnlyOwnSwap },
			Check:    commandtest.WantTitles("Song 1", "Song 2"),
		},
		{
			Name:     "a completed swap",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 3),
			Options:  swapOptions(1, 3),
			WantText: func(locale *messages.Locale) string { return locale.Queue.SwapCompleteTitle },
			Check:    commandtest.WantTitles("Song 3", "Song 2", "Song 1"),
		},
	})
}
