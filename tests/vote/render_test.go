package vote_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/vote"
	"noraegaori/tests/testutil/discordtest"
)

func TestVoteProgressEmbedShowsTheRecomputedQuorum(t *testing.T) {
	embed := vote.HookVoteProgressEmbed("g1", "Skip", "", "⏭", time.Now(), *vote.HookBuildTally(vote.HookTallyFields{Current: 2, Required: 3}))

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
	embed := vote.HookVoteProgressEmbed("g1", "Skip", "", "⏭", time.Now().Add(-2**vote.HookVoteExpirationTime), *vote.HookBuildTally(vote.HookTallyFields{Current: 1, Required: 2}))

	if !strings.HasSuffix(embed.Footer.Text, "expires in 0s") {
		t.Errorf("footer = %q, want the remaining seconds clamped at zero", embed.Footer.Text)
	}
}

func TestRenderVoteResultAppendsTheRecomputedTally(t *testing.T) {
	stubs := stubVoteEffects(t)

	session := vote.HookNewVoteSession("g1", vote.KindStop, "Stop", "⏹", "voice1", 5)
	*session.HookMessageID() = "msg1"
	*session.HookChannelID() = "chan1"

	vote.RenderResult(&discordgo.Session{}, session, messages.CreateSuccessEmbed("Stopped", "done"), *vote.HookBuildTally(vote.HookTallyFields{Current: 2, Required: 2, Passed: true}))

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

	session := vote.HookNewVoteSession("g1", vote.KindStop, "Stop", "⏹", "voice1", 2)
	*session.HookMessageID() = "msg1"
	*session.HookChannelID() = "chan1"

	vote.RenderFailure(&discordgo.Session{}, session, "Stop failed", "player exploded")

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
	api, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	unposted := vote.HookNewVoteSession("g1", vote.KindSkip, "Skip", "⏭", "voice1", 2)
	posted := vote.HookNewVoteSession("g1", vote.KindSkip, "Skip", "⏭", "voice1", 2)
	*posted.HookMessageID(), *posted.HookChannelID() = "msg1", "chan1"

	vote.HookRenderVoteEnded(api, unposted, vote.HookVoteEndCancelled)
	if sent := requests(); len(sent) != 0 {
		t.Fatalf("sent %v for a vote that was never posted", sent)
	}

	vote.HookRenderVoteEnded(api, posted, vote.HookVoteEndCancelled)
	if sent := requests(); len(sent) != 1 || sent[0].Method != http.MethodPatch || sent[0].Path != "/channels/chan1/messages/msg1" {
		t.Errorf("sent %v, want one edit of the posted vote", sent)
	}
}

func TestVoteProgressEmbedShowsRequesterAgreement(t *testing.T) {
	without := vote.HookVoteProgressEmbed("g1", "Remove", "Remove it?", "❌", time.Now(), *vote.HookBuildTally(vote.HookTallyFields{Current: 1, Required: 3}))
	if len(without.Fields) != 1 {
		t.Errorf("got %d fields with no requesters, want only the vote counter", len(without.Fields))
	}

	with := vote.HookVoteProgressEmbed("g1", "Remove", "Remove it?", "❌", time.Now(), *vote.HookBuildTally(vote.HookTallyFields{Current: 1, Required: 3, AdderVotes: 1, AdderTotal: 2}))
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

	session := vote.HookNewVoteSession("g1", vote.KindRemove, "Remove", "❌", "voice1", 3)
	*session.HookMessageID() = "msg1"
	*session.HookChannelID() = "chan1"

	vote.RenderResult(&discordgo.Session{}, session, messages.CreateSuccessEmbed("Removed", "done"),
		*vote.HookBuildTally(vote.HookTallyFields{Current: 1, Required: 3, AdderVotes: 1, AdderTotal: 1, Passed: true, ByAdderConsent: true}))

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want one result render", len(edits))
	}
	if edits[0].embed.Footer == nil || edits[0].embed.Footer.Text != messages.T("g1").Votes.AllAddersAgreed {
		t.Error("a consent pass did not explain itself in the footer")
	}
}
