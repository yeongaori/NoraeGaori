package commands

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func votingSession(t *testing.T, guildID string, listeners ...string) *discordgo.Session {
	t.Helper()

	states := make([]*discordgo.VoiceState, 0, len(listeners))
	for _, userID := range listeners {
		states = append(states, &discordgo.VoiceState{
			GuildID:   guildID,
			UserID:    userID,
			ChannelID: "voice1",
			Member:    &discordgo.Member{GuildID: guildID, User: &discordgo.User{ID: userID}},
		})
	}

	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "bot"}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: guildID, VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	return session
}

func reactionAdd(guildID, userID, messageID, emoji string) *discordgo.MessageReactionAdd {
	return &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			GuildID:   guildID,
			UserID:    userID,
			MessageID: messageID,
			Emoji:     discordgo.Emoji{Name: emoji},
		},
	}
}

func reactionRemove(guildID, userID, messageID, emoji string) *discordgo.MessageReactionRemove {
	return &discordgo.MessageReactionRemove{
		MessageReaction: &discordgo.MessageReaction{
			GuildID:   guildID,
			UserID:    userID,
			MessageID: messageID,
			Emoji:     discordgo.Emoji{Name: emoji},
		},
	}
}

func votingVote(t *testing.T, guildID string, requiredVotes int, onPassed func(*discordgo.Session, *voteSession, voteTally)) *voteSession {
	t.Helper()

	session := newVoteSession(guildID, voteKindSkip, "Skip", "⏭", "voice1", requiredVotes)
	session.onPassed = onPassed
	if _, claimed := activeVotes.claim(session); !claimed {
		t.Fatalf("failed to seed a vote for guild %s", guildID)
	}
	if !activeVotes.attachMessage(session, "msg-"+guildID, "chan-"+guildID) {
		t.Fatalf("failed to attach a message for guild %s", guildID)
	}
	t.Cleanup(func() { activeVotes.release(session) })
	return session
}

func TestReactionAddRejectsAnIneligibleVoter(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-ineligible", "u1", "u2", "u3")
	vote := votingVote(t, "dispatch-ineligible", 2, nil)

	onVoteReactionAdd(discord, reactionAdd("dispatch-ineligible", "outsider", vote.messageID, "⏭"))

	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	if len(stubs.removed) != 1 || stubs.removed[0] != "outsider" {
		t.Errorf("removed reactions = %v, want the outsider's reaction pulled", stubs.removed)
	}
	if len(stubs.edits) != 0 {
		t.Errorf("got %d edits, want none for a rejected reaction", len(stubs.edits))
	}
	if len(vote.votes) != 0 {
		t.Errorf("tally = %d, want the ineligible vote uncounted", len(vote.votes))
	}
}

func TestReactionAddBelowQuorumRendersProgress(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-progress", "u1", "u2", "u3")

	passed := false
	vote := votingVote(t, "dispatch-progress", 2, func(*discordgo.Session, *voteSession, voteTally) { passed = true })

	onVoteReactionAdd(discord, reactionAdd("dispatch-progress", "u1", vote.messageID, "⏭"))

	if passed {
		t.Error("onPassed ran before the quorum was reached")
	}

	edits := stubs.snapshotEdits()
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want one progress render", len(edits))
	}
	if field := edits[0].embed.Fields[0].Value; field != "1/2" {
		t.Errorf("progress field = %q, want 1/2", field)
	}
}

func TestReactionAddReachingQuorumRunsThePassPath(t *testing.T) {
	stubVoteEffects(t)
	discord := votingSession(t, "dispatch-pass", "u1", "u2", "u3")

	var passes atomic.Int32
	var seen voteTally
	vote := votingVote(t, "dispatch-pass", 2, func(_ *discordgo.Session, _ *voteSession, tally voteTally) {
		passes.Add(1)
		seen = tally
	})

	onVoteReactionAdd(discord, reactionAdd("dispatch-pass", "u1", vote.messageID, "⏭"))
	onVoteReactionAdd(discord, reactionAdd("dispatch-pass", "u2", vote.messageID, "⏭"))

	if passes.Load() != 1 {
		t.Fatalf("onPassed ran %d times, want exactly 1", passes.Load())
	}
	if !seen.passed || seen.current != 2 || seen.required != 2 {
		t.Errorf("passing tally = %+v, want 2 of 2 passed", seen)
	}
	if reason := <-vote.done; reason != voteEndPassed {
		t.Errorf("done delivered %v, want voteEndPassed", reason)
	}
	if _, live := activeVotes.snapshotOf("dispatch-pass", voteKindSkip); live {
		t.Error("the passed vote is still registered")
	}
	if activeVotes.sessionForMessage(vote.messageID) != nil {
		t.Error("the passed vote is still indexed by message")
	}
}

func TestConcurrentReactionsPassTheVoteOnce(t *testing.T) {
	stubVoteEffects(t)
	discord := votingSession(t, "dispatch-race", "u1", "u2", "u3", "u4")

	var passes atomic.Int32
	vote := votingVote(t, "dispatch-race", 2, func(*discordgo.Session, *voteSession, voteTally) { passes.Add(1) })

	var wg sync.WaitGroup
	for _, userID := range []string{"u1", "u2", "u3", "u4"} {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			onVoteReactionAdd(discord, reactionAdd("dispatch-race", userID, vote.messageID, "⏭"))
		}(userID)
	}
	wg.Wait()

	if passes.Load() != 1 {
		t.Errorf("onPassed ran %d times under concurrent reactions, want exactly 1", passes.Load())
	}
}

func TestReactionAddIgnoresARepeatVoter(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-repeat", "u1", "u2", "u3")
	vote := votingVote(t, "dispatch-repeat", 3, nil)

	onVoteReactionAdd(discord, reactionAdd("dispatch-repeat", "u1", vote.messageID, "⏭"))
	onVoteReactionAdd(discord, reactionAdd("dispatch-repeat", "u1", vote.messageID, "⏭"))

	if edits := stubs.snapshotEdits(); len(edits) != 1 {
		t.Errorf("got %d edits, want one because the repeat vote changes nothing", len(edits))
	}
}

func TestReactionRemoveWithdrawsAndRenders(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-withdraw", "u1", "u2", "u3", "u4", "u5")
	vote := votingVote(t, "dispatch-withdraw", 3, nil)

	onVoteReactionAdd(discord, reactionAdd("dispatch-withdraw", "u1", vote.messageID, "⏭"))
	onVoteReactionAdd(discord, reactionAdd("dispatch-withdraw", "u2", vote.messageID, "⏭"))
	onVoteReactionRemove(discord, reactionRemove("dispatch-withdraw", "u2", vote.messageID, "⏭"))

	edits := stubs.snapshotEdits()
	if len(edits) != 3 {
		t.Fatalf("got %d edits, want three renders", len(edits))
	}
	if field := edits[2].embed.Fields[0].Value; field != "1/3" {
		t.Errorf("post-withdrawal field = %q, want 1/3", field)
	}
}

func TestReactionRemoveIgnoresANonVoter(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-nonvoter", "u1", "u2", "u3")
	vote := votingVote(t, "dispatch-nonvoter", 2, nil)

	onVoteReactionRemove(discord, reactionRemove("dispatch-nonvoter", "u1", vote.messageID, "⏭"))

	if edits := stubs.snapshotEdits(); len(edits) != 0 {
		t.Errorf("got %d edits, want none for a withdrawal that changes nothing", len(edits))
	}
}

func TestReactionAddPassesWhenTheChannelEmpties(t *testing.T) {
	stubVoteEffects(t)
	discord := votingSession(t, "dispatch-shrink", "u1")

	var passes atomic.Int32
	var seen voteTally
	vote := votingVote(t, "dispatch-shrink", 3, func(_ *discordgo.Session, _ *voteSession, tally voteTally) {
		passes.Add(1)
		seen = tally
	})

	onVoteReactionAdd(discord, reactionAdd("dispatch-shrink", "u1", vote.messageID, "⏭"))

	if passes.Load() != 1 {
		t.Fatalf("onPassed ran %d times, want 1 once the recomputed quorum dropped to 1", passes.Load())
	}
	if seen.required != 1 {
		t.Errorf("passing tally required = %d, want the recomputed 1 rather than the seeded 3", seen.required)
	}
}

func TestAbsentRequesterCanConsentWithoutMovingTheQuorum(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"absentOwner"})

	discord := votingSession(t, "dispatch-consent", "u1", "u2", "u3")

	var passes atomic.Int32
	var seen voteTally
	vote := votingVote(t, "dispatch-consent", 2, func(_ *discordgo.Session, _ *voteSession, tally voteTally) {
		passes.Add(1)
		seen = tally
	})

	onVoteReactionAdd(discord, reactionAdd("dispatch-consent", "absentOwner", vote.messageID, "⏭"))

	if passes.Load() != 1 {
		t.Fatalf("onPassed ran %d times, want 1 once the only requester consented", passes.Load())
	}
	if !seen.byAdderConsent {
		t.Error("the pass was not attributed to requester consent")
	}
	if seen.current != 0 {
		t.Errorf("quorum count = %d, want 0 because the requester is not in the channel", seen.current)
	}
	if seen.adderVotes != 1 || seen.adderTotal != 1 {
		t.Errorf("requester counter = %d/%d, want 1/1", seen.adderVotes, seen.adderTotal)
	}
}

func TestAbsentNonRequesterIsStillRejected(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"absentOwner"})

	discord := votingSession(t, "dispatch-outsider", "u1", "u2")
	vote := votingVote(t, "dispatch-outsider", 2, nil)

	onVoteReactionAdd(discord, reactionAdd("dispatch-outsider", "randomAbsentee", vote.messageID, "⏭"))

	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	if len(stubs.removed) != 1 || stubs.removed[0] != "randomAbsentee" {
		t.Errorf("removed = %v, want the absent non-requester's reaction pulled", stubs.removed)
	}
}

func TestConsentNeedsEveryRequester(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"ownerA", "ownerB"})

	discord := votingSession(t, "dispatch-two-owners", "u1", "u2", "u3", "u4", "u5")

	var passes atomic.Int32
	vote := votingVote(t, "dispatch-two-owners", 3, func(*discordgo.Session, *voteSession, voteTally) { passes.Add(1) })

	onVoteReactionAdd(discord, reactionAdd("dispatch-two-owners", "ownerA", vote.messageID, "⏭"))
	if passes.Load() != 0 {
		t.Fatal("the vote passed with only one of two requesters consenting")
	}

	onVoteReactionAdd(discord, reactionAdd("dispatch-two-owners", "ownerB", vote.messageID, "⏭"))
	if passes.Load() != 1 {
		t.Errorf("onPassed ran %d times after both requesters consented, want 1", passes.Load())
	}
}

func TestRequesterWithdrawalDropsConsent(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"ownerA", "ownerB"})

	discord := votingSession(t, "dispatch-withdraw-consent", "u1", "u2", "u3", "u4", "u5")

	var passes atomic.Int32
	vote := votingVote(t, "dispatch-withdraw-consent", 3, func(*discordgo.Session, *voteSession, voteTally) { passes.Add(1) })

	onVoteReactionAdd(discord, reactionAdd("dispatch-withdraw-consent", "ownerA", vote.messageID, "⏭"))
	onVoteReactionRemove(discord, reactionRemove("dispatch-withdraw-consent", "ownerA", vote.messageID, "⏭"))
	onVoteReactionAdd(discord, reactionAdd("dispatch-withdraw-consent", "ownerB", vote.messageID, "⏭"))

	if passes.Load() != 0 {
		t.Error("the vote passed even though a requester withdrew their consent")
	}
	if len(vote.adderVotes) != 1 {
		t.Errorf("adder votes = %d, want only ownerB", len(vote.adderVotes))
	}
}

func TestPresentRequesterCountsForBothPaths(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"u2"})

	discord := votingSession(t, "dispatch-present-owner", "u1", "u2", "u3", "u4", "u5")

	var seen voteTally
	vote := votingVote(t, "dispatch-present-owner", 3, func(_ *discordgo.Session, _ *voteSession, tally voteTally) {
		seen = tally
	})

	onVoteReactionAdd(discord, reactionAdd("dispatch-present-owner", "u2", vote.messageID, "⏭"))

	if seen.current != 1 {
		t.Errorf("quorum count = %d, want 1 because the requester is in the channel", seen.current)
	}
	if !seen.byAdderConsent {
		t.Error("the present requester's consent did not pass the vote")
	}
}
