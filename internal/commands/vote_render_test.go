package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
)

func TestVoteProgressEmbedShowsTheRecomputedQuorum(t *testing.T) {
	embed := voteProgressEmbed("g1", "Skip", "", "⏭", time.Now(), voteTally{current: 2, required: 3})

	if len(embed.Fields) != 1 {
		t.Fatalf("got %d fields, want one vote counter", len(embed.Fields))
	}
	if embed.Fields[0].Value != "2/3" {
		t.Errorf("counter = %q, want 2/3", embed.Fields[0].Value)
	}
	if !strings.Contains(embed.Footer.Text, "⏭") {
		t.Errorf("footer = %q, want the vote emoji in it", embed.Footer.Text)
	}
}

func TestVoteProgressEmbedClampsAnExpiredClock(t *testing.T) {
	embed := voteProgressEmbed("g1", "Skip", "", "⏭", time.Now().Add(-2*voteExpirationTime), voteTally{current: 1, required: 2})

	if strings.Contains(embed.Footer.Text, "-") {
		t.Errorf("footer = %q, want the remaining seconds clamped at zero", embed.Footer.Text)
	}
}

func TestRenderVoteResultAppendsTheRecomputedTally(t *testing.T) {
	stubs := stubVoteEffects(t)

	session := newVoteSession("g1", voteKindStop, "Stop", "⏹", "voice1", 5)
	session.messageID = "msg1"
	session.channelID = "chan1"

	renderVoteResult(&discordgo.Session{}, session, messages.CreateSuccessEmbed("Stopped", "done"), voteTally{current: 2, required: 2, passed: true})

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want one result render", len(edits))
	}

	fields := edits[0].embed.Fields
	if len(fields) != 1 || fields[0].Value != "2/2" {
		t.Errorf("result fields = %+v, want a single 2/2 counter rather than the seeded 5", fields)
	}
}

func TestRenderVoteFailureUsesAnErrorEmbed(t *testing.T) {
	stubs := stubVoteEffects(t)

	session := newVoteSession("g1", voteKindStop, "Stop", "⏹", "voice1", 2)
	session.messageID = "msg1"
	session.channelID = "chan1"

	renderVoteFailure(&discordgo.Session{}, session, "Stop failed", "player exploded")

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want one failure render", len(edits))
	}
	if edits[0].embed.Color != messages.ColorError {
		t.Errorf("failure colour = %d, want the error colour %d", edits[0].embed.Color, messages.ColorError)
	}
	if edits[0].embed.Description != "player exploded" {
		t.Errorf("failure description = %q, want the player error", edits[0].embed.Description)
	}
}

func TestEditVoteMessageSkipsAnUnpostedVote(t *testing.T) {
	realEdit := editVoteMessage
	t.Cleanup(func() { editVoteMessage = realEdit })

	session := newVoteSession("g1", voteKindSkip, "Skip", "⏭", "voice1", 2)

	renderVoteEnded(&discordgo.Session{}, session, voteEndCancelled)
}

func TestSkipResultEmbedSwitchesOnAnEmptiedQueue(t *testing.T) {
	song := &queue.Song{Title: "Song", URL: "https://example.com/song", Thumbnail: "https://example.com/thumb.jpg"}

	skipped := skipResultEmbed("g1", song, false)
	ended := skipResultEmbed("g1", song, true)

	if skipped.Title == ended.Title {
		t.Errorf("both embeds titled %q, want the queue-ended variant to differ", skipped.Title)
	}
	if ended.Title != messages.T("g1").Music.PlaybackEndedTitle {
		t.Errorf("queue-ended title = %q, want the playback-ended title", ended.Title)
	}
	if skipped.Thumbnail == nil || skipped.Thumbnail.URL != song.Thumbnail {
		t.Error("the skip embed lost the song thumbnail")
	}
}

func TestVoteProgressEmbedShowsRequesterAgreement(t *testing.T) {
	without := voteProgressEmbed("g1", "Remove", "Remove it?", "❌", time.Now(), voteTally{current: 1, required: 3})
	if len(without.Fields) != 1 {
		t.Errorf("got %d fields with no requesters, want only the vote counter", len(without.Fields))
	}

	with := voteProgressEmbed("g1", "Remove", "Remove it?", "❌", time.Now(), voteTally{current: 1, required: 3, adderVotes: 1, adderTotal: 2})
	if len(with.Fields) != 2 {
		t.Fatalf("got %d fields, want the vote counter and the requester counter", len(with.Fields))
	}
	if with.Fields[1].Value != "1/2" {
		t.Errorf("requester counter = %q, want 1/2", with.Fields[1].Value)
	}
	if with.Description != "Remove it?" {
		t.Errorf("description = %q, want the vote prompt", with.Description)
	}
}

func TestRenderVoteResultNotesAdderConsent(t *testing.T) {
	stubs := stubVoteEffects(t)

	session := newVoteSession("g1", voteKindRemove, "Remove", "❌", "voice1", 3)
	session.messageID = "msg1"
	session.channelID = "chan1"

	renderVoteResult(&discordgo.Session{}, session, messages.CreateSuccessEmbed("Removed", "done"),
		voteTally{current: 1, required: 3, adderVotes: 1, adderTotal: 1, passed: true, byAdderConsent: true})

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want one result render", len(edits))
	}
	if edits[0].embed.Footer == nil || edits[0].embed.Footer.Text != messages.T("g1").Votes.AllAddersAgreed {
		t.Error("a consent pass did not explain itself in the footer")
	}
}
