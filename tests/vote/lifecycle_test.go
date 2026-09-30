package vote_test

import (
	"noraegaori/internal/discord"
	"noraegaori/tests/testutil"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/vote"
)

type recordedEdit struct {
	channelID string
	messageID string
	embed     *discordgo.MessageEmbed
}

type voteStubs struct {
	mu      sync.Mutex
	edits   []recordedEdit
	added   []string
	cleared []string
	removed []string
}

func stubVoteEffects(t *testing.T) *voteStubs {
	t.Helper()

	stubs := &voteStubs{}

	testutil.Swap(t, vote.HookEditVoteMessage, func(s *discordgo.Session, channelID, messageID string, embed *discordgo.MessageEmbed) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.edits = append(stubs.edits, recordedEdit{channelID: channelID, messageID: messageID, embed: embed})
	})
	testutil.Swap(t, &discord.AddPromptReaction, func(s *discordgo.Session, channelID, messageID, emoji string) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.added = append(stubs.added, messageID)
	})
	testutil.Swap(t, &discord.ClearPromptReactions, func(s *discordgo.Session, channelID, messageID string) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.cleared = append(stubs.cleared, messageID)
	})
	testutil.Swap(t, &discord.RemoveUserReaction, func(s *discordgo.Session, channelID, messageID, emoji, userID string) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.removed = append(stubs.removed, userID)
	})

	testutil.Swap(t, vote.HookAddersFor, func(string, vote.Target) []string { return nil })

	return stubs
}

func (v *voteStubs) useAdders(adders []string) {
	*vote.HookAddersFor = func(string, vote.Target) []string { return adders }
}

func (v *voteStubs) shortenExpiry(t *testing.T, d time.Duration) {
	t.Helper()

	testutil.Swap(t, vote.HookVoteExpirationTime, d)
}

func (v *voteStubs) snapshotEdits() []recordedEdit {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]recordedEdit(nil), v.edits...)
}

func (v *voteStubs) clearedMessages() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.cleared...)
}

func liveVote(t *testing.T, guildID string, kind vote.Kind) *vote.Session {
	t.Helper()

	session := vote.HookNewVoteSession(guildID, kind, "Vote", "⏭", "voice1", 2)
	if _, claimed := (*vote.HookActiveVotes).HookClaim(session); !claimed {
		t.Fatalf("failed to seed a vote for guild %s", guildID)
	}
	if !(*vote.HookActiveVotes).HookAttachMessage(session, "msg-"+guildID, "chan-"+guildID) {
		t.Fatalf("failed to attach a message for guild %s", guildID)
	}
	t.Cleanup(func() { (*vote.HookActiveVotes).HookRelease(session) })
	return session
}

func TestAwaitVoteOutcomeRendersACancellation(t *testing.T) {
	stubs := stubVoteEffects(t)
	session := liveVote(t, "lifecycle-cancel", vote.KindSkip)

	done := make(chan struct{})
	go func() {
		vote.HookAwaitVoteOutcome(&discordgo.Session{}, session)
		close(done)
	}()

	(*vote.HookActiveVotes).HookCancel("lifecycle-cancel", vote.HookVoteEndSuperseded, vote.KindSkip)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitVoteOutcome did not return after the vote was cancelled")
	}

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want exactly the ending notice", len(edits))
	}
	if want := vote.HookVoteEndDescription("", vote.HookVoteEndSuperseded); edits[0].embed.Description != want {
		t.Errorf("ending description = %q, want %q", edits[0].embed.Description, want)
	}
	if cleared := stubs.clearedMessages(); len(cleared) != 1 || cleared[0] != *session.HookMessageID() {
		t.Errorf("cleared = %v, want the vote message cleared once", cleared)
	}
}

func TestAwaitVoteOutcomeStaysQuietWhenTheVotePassed(t *testing.T) {
	stubs := stubVoteEffects(t)
	session := liveVote(t, "lifecycle-passed", vote.KindSkip)

	done := make(chan struct{})
	go func() {
		vote.HookAwaitVoteOutcome(&discordgo.Session{}, session)
		close(done)
	}()

	session.HookEndWith(vote.HookVoteEndPassed)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitVoteOutcome did not return after the vote passed")
	}

	if edits := stubs.snapshotEdits(); len(edits) != 0 {
		t.Errorf("got %d edits, want none because onPassed renders the result", len(edits))
	}
	if cleared := stubs.clearedMessages(); len(cleared) != 1 {
		t.Errorf("cleared = %v, want the reactions cleared once", cleared)
	}
}

func TestAwaitVoteOutcomeExpires(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.shortenExpiry(t, 20*time.Millisecond)

	session := liveVote(t, "lifecycle-expiry", vote.KindStop)

	done := make(chan struct{})
	go func() {
		vote.HookAwaitVoteOutcome(&discordgo.Session{}, session)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitVoteOutcome did not return after the expiry")
	}

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want exactly the expiry notice", len(edits))
	}
	if want := vote.HookVoteEndDescription("", vote.HookVoteEndExpired); edits[0].embed.Description != want {
		t.Errorf("expiry description = %q, want %q", edits[0].embed.Description, want)
	}
	if _, live := (*vote.HookActiveVotes).HookSnapshotOf("lifecycle-expiry", vote.KindStop); live {
		t.Error("the expired vote is still registered")
	}
}

func TestAwaitVoteOutcomeSeedsItsOwnReaction(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.shortenExpiry(t, 10*time.Millisecond)

	session := liveVote(t, "lifecycle-seed", vote.KindSkip)

	vote.HookAwaitVoteOutcome(&discordgo.Session{}, session)

	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	if len(stubs.added) != 1 || stubs.added[0] != *session.HookMessageID() {
		t.Errorf("seeded reactions = %v, want the vote message seeded once", stubs.added)
	}
}

func TestExpiredTimerDoesNotOverwriteAResolvedVote(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.shortenExpiry(t, 10*time.Millisecond)

	session := liveVote(t, "lifecycle-resolved", vote.KindSkip)
	if !(*vote.HookActiveVotes).HookResolve(session) {
		t.Fatal("failed to resolve the vote before the timer fired")
	}

	vote.HookAwaitVoteOutcome(&discordgo.Session{}, session)

	if edits := stubs.snapshotEdits(); len(edits) != 0 {
		t.Errorf("got %d edits, want none for a vote that already resolved", len(edits))
	}
}

func TestEndedBeforeAttachPrefersTheRecordedReason(t *testing.T) {
	session := vote.HookNewVoteSession("g1", vote.KindSkip, "Vote", "⏭", "voice1", 2)

	if reason := vote.HookEndedBeforeAttach(session); reason != vote.HookVoteEndCancelled {
		t.Errorf("endedBeforeAttach with no reason = %v, want voteEndCancelled", reason)
	}

	session.HookEndWith(vote.HookVoteEndQueueEnded)
	if reason := vote.HookEndedBeforeAttach(session); reason != vote.HookVoteEndQueueEnded {
		t.Errorf("endedBeforeAttach = %v, want the recorded voteEndQueueEnded", reason)
	}
}
