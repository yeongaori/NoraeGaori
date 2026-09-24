package queue

import (
	"slices"
	"strconv"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/vote"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/queuetest"
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

	q.Playing, q.Loading = false, true
	if songs := removableFrom(q, []int{1, 2}); len(songs) != 1 || songs[0].ID != 2 {
		t.Errorf("removable = %v, want only song 2 while song 1 is loading", songs)
	}

	idle := queuetest.QueueOf("a", "b")
	if songs := removableFrom(idle, []int{1}); len(songs) != 1 {
		t.Errorf("removable = %v, want the head removable while nothing is playing", songs)
	}

	if songs := removableFrom(nil, []int{1}); songs != nil {
		t.Errorf("removable = %v, want nothing from a nil queue", songs)
	}
}

func seededRemoveVote(t *testing.T) (int, func() []int) {
	t.Helper()

	fixture := commandtest.NewFixture(t, nil, queuetest.SongsBy(commandtest.OtherID, 2)...)
	target := fixture.Queue.Songs[1].ID
	return target, func() []int {
		var results []int
		result := func(removed int) *discordgo.MessageEmbed {
			results = append(results, removed)
			return messages.CreateSuccessEmbed("removed", strconv.Itoa(removed))
		}
		applyRemoveVote(commandtest.GuildID, []int{target}, result)(fixture.Session, &vote.Session{}, vote.Tally{})
		return results
	}
}

func TestApplyingARemoveVote(t *testing.T) {
	t.Run("removes the target", func(t *testing.T) {
		_, apply := seededRemoveVote(t)
		if results := apply(); !slices.Equal(results, []int{1}) {
			t.Errorf("the result was built for %v removals, want one for 1", results)
		}
		commandtest.WantTitles("Song 1")(t, nil)
	})

	t.Run("leaves the queue alone when the target is already gone", func(t *testing.T) {
		target, apply := seededRemoveVote(t)
		if err := queue.RemoveSongsByIDs(commandtest.GuildID, []int{target}); err != nil {
			t.Fatalf("failed to remove the target early: %v", err)
		}
		queue.InvalidateCache(commandtest.GuildID)

		if results := apply(); len(results) != 0 {
			t.Errorf("a vote on a vanished song reported %v removals", results)
		}
		commandtest.WantTitles("Song 1")(t, nil)
	})

	t.Run("keeps the songs when the removal fails", func(t *testing.T) {
		_, apply := seededRemoveVote(t)
		var results []int
		dbtest.WhileClosed(t, func() { results = apply() })
		if len(results) != 0 {
			t.Errorf("a failed removal reported %v removals", results)
		}
		commandtest.WantTitles("Song 1", "Song 2")(t, nil)
	})
}
