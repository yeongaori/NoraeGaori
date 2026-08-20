package commands

import (
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type recordedEdit struct {
	channelID string
	messageID string
	embed     *discordgo.MessageEmbed
}

type voteStubs struct {
	mu       sync.Mutex
	edits    []recordedEdit
	added    []string
	cleared  []string
	removed  []string
	restores []func()
}

func stubVoteEffects(t *testing.T) *voteStubs {
	t.Helper()

	stubs := &voteStubs{}

	realEdit := editVoteMessage
	realAdd := addPromptReaction
	realClear := clearPromptReactions
	realRemove := removeUserReaction

	editVoteMessage = func(s *discordgo.Session, channelID, messageID string, embed *discordgo.MessageEmbed) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.edits = append(stubs.edits, recordedEdit{channelID: channelID, messageID: messageID, embed: embed})
	}
	addPromptReaction = func(s *discordgo.Session, channelID, messageID, emoji string) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.added = append(stubs.added, messageID)
	}
	clearPromptReactions = func(s *discordgo.Session, channelID, messageID string) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.cleared = append(stubs.cleared, messageID)
	}
	removeUserReaction = func(s *discordgo.Session, channelID, messageID, emoji, userID string) {
		stubs.mu.Lock()
		defer stubs.mu.Unlock()
		stubs.removed = append(stubs.removed, userID)
	}

	realAdders := addersFor
	addersFor = func(string, voteTarget) []string { return nil }

	t.Cleanup(func() {
		addersFor = realAdders
		editVoteMessage = realEdit
		addPromptReaction = realAdd
		clearPromptReactions = realClear
		removeUserReaction = realRemove
		for _, restore := range stubs.restores {
			restore()
		}
	})

	return stubs
}

func (v *voteStubs) useAdders(adders []string) {
	addersFor = func(string, voteTarget) []string { return adders }
}

func (v *voteStubs) shortenExpiry(t *testing.T, d time.Duration) {
	t.Helper()

	real := voteExpirationTime
	voteExpirationTime = d
	v.restores = append(v.restores, func() { voteExpirationTime = real })
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

func liveVote(t *testing.T, guildID string, kind voteKind) *voteSession {
	t.Helper()

	session := newVoteSession(guildID, kind, "Vote", "⏭", "voice1", 2)
	if _, claimed := activeVotes.claim(session); !claimed {
		t.Fatalf("failed to seed a vote for guild %s", guildID)
	}
	if !activeVotes.attachMessage(session, "msg-"+guildID, "chan-"+guildID) {
		t.Fatalf("failed to attach a message for guild %s", guildID)
	}
	t.Cleanup(func() { activeVotes.release(session) })
	return session
}

func TestAwaitVoteOutcomeRendersACancellation(t *testing.T) {
	stubs := stubVoteEffects(t)
	session := liveVote(t, "lifecycle-cancel", voteKindSkip)

	done := make(chan struct{})
	go func() {
		awaitVoteOutcome(&discordgo.Session{}, session)
		close(done)
	}()

	activeVotes.cancel("lifecycle-cancel", voteEndSuperseded, voteKindSkip)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitVoteOutcome did not return after the vote was cancelled")
	}

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want exactly the ending notice", len(edits))
	}
	if want := voteEndDescription("", voteEndSuperseded); edits[0].embed.Description != want {
		t.Errorf("ending description = %q, want %q", edits[0].embed.Description, want)
	}
	if cleared := stubs.clearedMessages(); len(cleared) != 1 || cleared[0] != session.messageID {
		t.Errorf("cleared = %v, want the vote message cleared once", cleared)
	}
}

func TestAwaitVoteOutcomeStaysQuietWhenTheVotePassed(t *testing.T) {
	stubs := stubVoteEffects(t)
	session := liveVote(t, "lifecycle-passed", voteKindSkip)

	done := make(chan struct{})
	go func() {
		awaitVoteOutcome(&discordgo.Session{}, session)
		close(done)
	}()

	session.endWith(voteEndPassed)

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

	session := liveVote(t, "lifecycle-expiry", voteKindStop)

	done := make(chan struct{})
	go func() {
		awaitVoteOutcome(&discordgo.Session{}, session)
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
	if want := voteEndDescription("", voteEndExpired); edits[0].embed.Description != want {
		t.Errorf("expiry description = %q, want %q", edits[0].embed.Description, want)
	}
	if _, live := activeVotes.snapshotOf("lifecycle-expiry", voteKindStop); live {
		t.Error("the expired vote is still registered")
	}
}

func TestAwaitVoteOutcomeSeedsItsOwnReaction(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.shortenExpiry(t, 10*time.Millisecond)

	session := liveVote(t, "lifecycle-seed", voteKindSkip)

	awaitVoteOutcome(&discordgo.Session{}, session)

	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	if len(stubs.added) != 1 || stubs.added[0] != session.messageID {
		t.Errorf("seeded reactions = %v, want the vote message seeded once", stubs.added)
	}
}

func TestExpiredTimerDoesNotOverwriteAResolvedVote(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.shortenExpiry(t, 10*time.Millisecond)

	session := liveVote(t, "lifecycle-resolved", voteKindSkip)
	if !activeVotes.resolve(session) {
		t.Fatal("failed to resolve the vote before the timer fired")
	}

	awaitVoteOutcome(&discordgo.Session{}, session)

	if edits := stubs.snapshotEdits(); len(edits) != 0 {
		t.Errorf("got %d edits, want none for a vote that already resolved", len(edits))
	}
}

func TestEndedBeforeAttachPrefersTheRecordedReason(t *testing.T) {
	session := newVoteSession("g1", voteKindSkip, "Vote", "⏭", "voice1", 2)

	if reason := endedBeforeAttach(session); reason != voteEndCancelled {
		t.Errorf("endedBeforeAttach with no reason = %v, want voteEndCancelled", reason)
	}

	session.endWith(voteEndQueueEnded)
	if reason := endedBeforeAttach(session); reason != voteEndQueueEnded {
		t.Errorf("endedBeforeAttach = %v, want the recorded voteEndQueueEnded", reason)
	}
}
