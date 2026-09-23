package queue

import (
	"fmt"
	"testing"

	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/queuetest"
)

func TestSkipTo(t *testing.T) {
	inVoice := []string{commandtest.CallerID}

	commandtest.Run(t, "skipto", HandleSkipTo, []commandtest.Case{
		{
			Name:       "a missing position",
			VoiceUsers: inVoice,
			WantText:   func(locale *messages.Locale) string { return locale.Queue.SkipToEnterPosition },
		},
		{
			Name:     "a caller outside voice",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 2),
			Options:  skipToOption(2),
			WantText: func(locale *messages.Locale) string { return locale.Music.EnterVoiceChannel },
		},
		{
			Name:       "an empty queue",
			VoiceUsers: inVoice,
			Options:    skipToOption(2),
			WantText:   emptyQueueText,
		},
		{
			Name:       "a position past the queue",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 2),
			VoiceUsers: inVoice,
			Options:    skipToOption(5),
			WantText:   func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 2) },
		},
		{
			Name:       "the current song",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 2),
			VoiceUsers: inVoice,
			Options:    skipToOption(1),
			WantText:   func(locale *messages.Locale) string { return locale.Queue.SkipToCurrent },
		},
		{
			Name:       "a completed skip",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 3),
			VoiceUsers: inVoice,
			Options:    skipToOption(3),
			WantText:   func(locale *messages.Locale) string { return locale.Queue.SkipToCompleteTitle },
			Check:      commandtest.WantTitles("Song 3"),
		},
	})
}
