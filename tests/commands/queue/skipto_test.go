package queue_test

import (
	"fmt"
	"testing"

	queuecommand "noraegaori/internal/commands/queue"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestSkipToUsesCheckedSong(t *testing.T) {
	caller := commandtest.CallerID
	fixture := commandtest.NewFixture(t, []string{caller}, queuetest.SongsBy(caller, 5)...)
	advanceQueueBehindCache(t)

	_ = queuecommand.HandleSkipTo(fixture.Session,
		discordtest.SlashInteraction(commandtest.GuildID, "skipto", discordtest.Member(commandtest.GuildID, caller), skipToOption(4)...))

	commandtest.WantTitles("Song 4", "Song 5")(t, nil)
}

func TestSkipToRefusesNewCurrentSong(t *testing.T) {
	caller := commandtest.CallerID
	fixture := commandtest.NewFixture(t, []string{caller}, queuetest.SongsBy(caller, 3)...)
	advanceQueueBehindCache(t)

	_ = queuecommand.HandleSkipTo(fixture.Session,
		discordtest.SlashInteraction(commandtest.GuildID, "skipto", discordtest.Member(commandtest.GuildID, caller), skipToOption(2)...))

	commandtest.WantReplyText(t, fixture.Requests(), messages.T(commandtest.GuildID).Queue.SkipToCurrent)
	commandtest.WantTitles("Song 2", "Song 3")(t, nil)
}

func TestSkipTo(t *testing.T) {
	inVoice := []string{commandtest.CallerID}
	outOfRange := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.EnterValidRange, 3) }
	unchanged := commandtest.WantTitles("Song 1", "Song 2", "Song 3")

	commandtest.Run(t, "skipto", queuecommand.HandleSkipTo, []commandtest.Case{
		{
			Name:       "a missing position",
			VoiceUsers: inVoice,
			WantText:   func(locale *messages.Locale) string { return locale.Queue.SkipToEnterPosition },
		},
		{
			Name:     "a caller outside voice",
			Songs:    queuetest.SongsBy(commandtest.CallerID, 3),
			Options:  skipToOption(2),
			WantText: func(locale *messages.Locale) string { return locale.Music.EnterVoiceChannel },
			Check:    unchanged,
		},
		{
			Name:       "an empty queue",
			VoiceUsers: inVoice,
			Options:    skipToOption(2),
			WantText:   emptyQueueText,
		},
		{
			Name:       "position zero",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 3),
			VoiceUsers: inVoice,
			Options:    skipToOption(0),
			WantText:   outOfRange,
			Check:      unchanged,
		},
		{
			Name:       "the position just past the queue",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 3),
			VoiceUsers: inVoice,
			Options:    skipToOption(4),
			WantText:   outOfRange,
			Check:      unchanged,
		},
		{
			Name:       "the current song",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 3),
			VoiceUsers: inVoice,
			Options:    skipToOption(1),
			WantText:   func(locale *messages.Locale) string { return locale.Queue.SkipToCurrent },
			Check:      unchanged,
		},
		{
			Name:       "the last song",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 3),
			VoiceUsers: inVoice,
			Options:    skipToOption(3),
			WantText:   func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SkipToSongsCount, 2) },
			Check: func(t *testing.T, reply map[string]any) {
				commandtest.ReplyContains("Song 3")(t, reply)
				commandtest.WantTitles("Song 3")(t, reply)
			},
		},
	})
}
