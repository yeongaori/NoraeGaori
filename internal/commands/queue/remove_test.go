package queue

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/commandtest"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/queuetest"
	"noraegaori/internal/vote"
)

func TestSongIDsAndOwnership(t *testing.T) {
	q := queuetest.QueueOf("a", "b", "a")

	if ids := songIDsOf(q.Songs); len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Errorf("songIDsOf = %v, want 1, 2, 3", ids)
	}

	if !ownedBy(q.Songs[:1], "a") {
		t.Error("a caller's own song was not recognised")
	}
	if ownedBy(q.Songs, "a") {
		t.Error("a range containing another user's song was treated as owned")
	}
	if !ownedBy(nil, "a") {
		t.Error("an empty selection was not treated as owned")
	}
}

func TestRemovableFromDropsVanishedAndPlayingSongs(t *testing.T) {
	q := queuetest.QueueOf("a", "b", "c")
	q.Playing = true

	songs := removableFrom(q, []int{1, 2, 99})
	if len(songs) != 1 || songs[0].ID != 2 {
		t.Errorf("removable = %v, want only song 2: song 1 is playing and 99 is gone", songs)
	}

	if songs := removableFrom(q, []int{99}); len(songs) != 0 {
		t.Errorf("removable = %v, want nothing when every target is gone", songs)
	}

	idle := queuetest.QueueOf("a", "b")
	if songs := removableFrom(idle, []int{1}); len(songs) != 1 {
		t.Errorf("removable = %v, want the head removable while nothing is playing", songs)
	}

	if songs := removableFrom(nil, []int{1}); songs != nil {
		t.Errorf("removable = %v, want nothing from a nil queue", songs)
	}
}

func TestRemove(t *testing.T) {
	caller, other := commandtest.CallerID, commandtest.OtherID
	inVoice := []string{caller}
	invalidRange := func(locale *messages.Locale) string { return locale.Queue.InvalidRange }
	songRemoved := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SongRemoved, "Song 2") }
	rangeRemoved := func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.RangeRemoved, 2, 2, 3) }

	commandtest.Run(t, "remove", HandleRemove, []commandtest.Case{
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
			WantText: func(locale *messages.Locale) string { return fmt.Sprintf(locale.Queue.SongsRemovedAll, caller, 2) },
			Check:    commandtest.WantTitles("Song 2"),
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
			Name:     "a range over the playing song",
			Songs:    queuetest.SongsBy(caller, 3),
			Options:  removeOption("1-2"),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Queue.RangeIncludesCurrent },
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
			Name:     "the playing song",
			Songs:    queuetest.SongsBy(caller, 2),
			Options:  removeOption("1"),
			Prepare:  commandtest.Playing,
			WantText: func(locale *messages.Locale) string { return locale.Queue.CannotRemoveCurrent },
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

func removedEmbed(removed int) *discordgo.MessageEmbed {
	return messages.CreateSuccessEmbed("removed", strconv.Itoa(removed))
}

func seededRemoveVote(t *testing.T) (int, func()) {
	t.Helper()

	fixture := commandtest.NewFixture(t, nil, queuetest.SongsBy(commandtest.OtherID, 2)...)
	target := fixture.Queue.Songs[1].ID
	return target, func() {
		applyRemoveVote(commandtest.GuildID, []int{target}, removedEmbed)(fixture.Session, &vote.Session{}, vote.Tally{})
	}
}

func TestApplyingARemoveVote(t *testing.T) {
	t.Run("removes the target", func(t *testing.T) {
		_, apply := seededRemoveVote(t)
		apply()
		commandtest.WantTitles("Song 1")(t, nil)
	})

	t.Run("leaves the queue alone when the target is already gone", func(t *testing.T) {
		target, apply := seededRemoveVote(t)
		if err := queue.RemoveSongsByIDs(commandtest.GuildID, []int{target}); err != nil {
			t.Fatalf("failed to remove the target early: %v", err)
		}
		queue.InvalidateCache(commandtest.GuildID)

		apply()
		commandtest.WantTitles("Song 1")(t, nil)
	})

	t.Run("keeps the songs when the removal fails", func(t *testing.T) {
		_, apply := seededRemoveVote(t)
		dbtest.WhileClosed(t, apply)
		commandtest.WantTitles("Song 1", "Song 2")(t, nil)
	})
}
