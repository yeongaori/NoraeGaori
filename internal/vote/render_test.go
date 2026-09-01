package vote

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
)

func TestVoteProgressEmbedShowsTheRecomputedQuorum(t *testing.T) {
	embed := voteProgressEmbed("g1", "Skip", "", "⏭", time.Now(), Tally{current: 2, required: 3})

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
	embed := voteProgressEmbed("g1", "Skip", "", "⏭", time.Now().Add(-2*voteExpirationTime), Tally{current: 1, required: 2})

	if strings.Contains(embed.Footer.Text, "-") {
		t.Errorf("footer = %q, want the remaining seconds clamped at zero", embed.Footer.Text)
	}
}

func TestRenderVoteResultAppendsTheRecomputedTally(t *testing.T) {
	stubs := stubVoteEffects(t)

	session := newVoteSession("g1", KindStop, "Stop", "⏹", "voice1", 5)
	session.messageID = "msg1"
	session.channelID = "chan1"

	RenderResult(&discordgo.Session{}, session, messages.CreateSuccessEmbed("Stopped", "done"), Tally{current: 2, required: 2, passed: true})

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

	session := newVoteSession("g1", KindStop, "Stop", "⏹", "voice1", 2)
	session.messageID = "msg1"
	session.channelID = "chan1"

	RenderFailure(&discordgo.Session{}, session, "Stop failed", "player exploded")

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

	session := newVoteSession("g1", KindSkip, "Skip", "⏭", "voice1", 2)

	renderVoteEnded(&discordgo.Session{}, session, voteEndCancelled)
}

func TestVoteProgressEmbedShowsRequesterAgreement(t *testing.T) {
	without := voteProgressEmbed("g1", "Remove", "Remove it?", "❌", time.Now(), Tally{current: 1, required: 3})
	if len(without.Fields) != 1 {
		t.Errorf("got %d fields with no requesters, want only the vote counter", len(without.Fields))
	}

	with := voteProgressEmbed("g1", "Remove", "Remove it?", "❌", time.Now(), Tally{current: 1, required: 3, adderVotes: 1, adderTotal: 2})
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

	session := newVoteSession("g1", KindRemove, "Remove", "❌", "voice1", 3)
	session.messageID = "msg1"
	session.channelID = "chan1"

	RenderResult(&discordgo.Session{}, session, messages.CreateSuccessEmbed("Removed", "done"),
		Tally{current: 1, required: 3, adderVotes: 1, adderTotal: 1, passed: true, byAdderConsent: true})

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want one result render", len(edits))
	}
	if edits[0].embed.Footer == nil || edits[0].embed.Footer.Text != messages.T("g1").Votes.AllAddersAgreed {
		t.Error("a consent pass did not explain itself in the footer")
	}
}
