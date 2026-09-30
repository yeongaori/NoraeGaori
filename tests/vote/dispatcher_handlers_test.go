package vote_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/vote"
	"noraegaori/tests/testutil/discordtest"
)

func votingSession(t *testing.T, guildID string, listeners ...string) *discordgo.Session {
	t.Helper()

	states := make([]*discordgo.VoiceState, 0, len(listeners))
	for _, userID := range listeners {
		states = append(states, discordtest.VoiceState(guildID, userID, "voice1", false))
	}

	return discordtest.SessionWithGuild(t, "bot", guildID, states, nil)
}

func votingVote(t *testing.T, guildID string, requiredVotes int, onPassed func(*discordgo.Session, *vote.Session, vote.Tally)) *vote.Session {
	t.Helper()

	session := vote.HookNewVoteSession(guildID, vote.KindSkip, "Skip", "⏭", "voice1", requiredVotes)
	*session.HookOnPassed() = onPassed
	if _, claimed := (*vote.HookActiveVotes).HookClaim(session); !claimed {
		t.Fatalf("failed to seed a vote for guild %s", guildID)
	}
	if !(*vote.HookActiveVotes).HookAttachMessage(session, "msg-"+guildID, "chan-"+guildID) {
		t.Fatalf("failed to attach a message for guild %s", guildID)
	}
	t.Cleanup(func() { (*vote.HookActiveVotes).HookRelease(session) })
	return session
}

func TestReactionAddRejectsAnIneligibleVoter(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-ineligible", "u1", "u2", "u3")
	voteSession := votingVote(t, "dispatch-ineligible", 2, nil)

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-ineligible", "outsider", *voteSession.HookMessageID(), "⏭"))

	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	if len(stubs.removed) != 1 || stubs.removed[0] != "outsider" {
		t.Errorf("removed reactions = %v, want the outsider's reaction pulled", stubs.removed)
	}
	if len(stubs.edits) != 0 {
		t.Errorf("got %d edits, want none for a rejected reaction", len(stubs.edits))
	}
	if len(*voteSession.HookVotes()) != 0 {
		t.Errorf("tally = %d, want the ineligible vote uncounted", len(*voteSession.HookVotes()))
	}
}

func TestReactionAddBelowQuorumRendersProgress(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-progress", "u1", "u2", "u3")

	passed := false
	voteSession := votingVote(t, "dispatch-progress", 2, func(*discordgo.Session, *vote.Session, vote.Tally) { passed = true })

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-progress", "u1", *voteSession.HookMessageID(), "⏭"))

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
	var seen vote.Tally
	voteSession := votingVote(t, "dispatch-pass", 2, func(_ *discordgo.Session, _ *vote.Session, tally vote.Tally) {
		passes.Add(1)
		seen = tally
	})

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-pass", "u1", *voteSession.HookMessageID(), "⏭"))
	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-pass", "u2", *voteSession.HookMessageID(), "⏭"))

	if passes.Load() != 1 {
		t.Fatalf("onPassed ran %d times, want exactly 1", passes.Load())
	}
	if !*seen.HookPassed() || *seen.HookCurrent() != 2 || *seen.HookRequired() != 2 {
		t.Errorf("passing tally = %+v, want 2 of 2 passed", seen)
	}
	if reason := <-*voteSession.HookDone(); reason != vote.HookVoteEndPassed {
		t.Errorf("done delivered %v, want voteEndPassed", reason)
	}
	if _, live := (*vote.HookActiveVotes).HookSnapshotOf("dispatch-pass", vote.KindSkip); live {
		t.Error("the passed vote is still registered")
	}
	if (*vote.HookActiveVotes).HookSessionForMessage(*voteSession.HookMessageID()) != nil {
		t.Error("the passed vote is still indexed by message")
	}
}

func TestConcurrentReactionsPassTheVoteOnce(t *testing.T) {
	stubVoteEffects(t)
	discord := votingSession(t, "dispatch-race", "u1", "u2", "u3", "u4")

	var passes atomic.Int32
	voteSession := votingVote(t, "dispatch-race", 2, func(*discordgo.Session, *vote.Session, vote.Tally) { passes.Add(1) })

	var wg sync.WaitGroup
	for _, userID := range []string{"u1", "u2", "u3", "u4"} {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-race", userID, *voteSession.HookMessageID(), "⏭"))
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
	voteSession := votingVote(t, "dispatch-repeat", 3, nil)

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-repeat", "u1", *voteSession.HookMessageID(), "⏭"))
	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-repeat", "u1", *voteSession.HookMessageID(), "⏭"))

	if edits := stubs.snapshotEdits(); len(edits) != 1 {
		t.Errorf("got %d edits, want one because the repeat vote changes nothing", len(edits))
	}
}

func TestReactionRemoveWithdrawsAndRenders(t *testing.T) {
	stubs := stubVoteEffects(t)
	discord := votingSession(t, "dispatch-withdraw", "u1", "u2", "u3", "u4", "u5")
	voteSession := votingVote(t, "dispatch-withdraw", 3, nil)

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-withdraw", "u1", *voteSession.HookMessageID(), "⏭"))
	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-withdraw", "u2", *voteSession.HookMessageID(), "⏭"))
	vote.HookOnVoteReactionRemove(discord, discordtest.ReactionRemove("dispatch-withdraw", "u2", *voteSession.HookMessageID(), "⏭"))

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
	voteSession := votingVote(t, "dispatch-nonvoter", 2, nil)

	vote.HookOnVoteReactionRemove(discord, discordtest.ReactionRemove("dispatch-nonvoter", "u1", *voteSession.HookMessageID(), "⏭"))

	if edits := stubs.snapshotEdits(); len(edits) != 0 {
		t.Errorf("got %d edits, want none for a withdrawal that changes nothing", len(edits))
	}
}

func TestReactionAddPassesWhenTheChannelEmpties(t *testing.T) {
	stubVoteEffects(t)
	discord := votingSession(t, "dispatch-shrink", "u1")

	var passes atomic.Int32
	var seen vote.Tally
	voteSession := votingVote(t, "dispatch-shrink", 3, func(_ *discordgo.Session, _ *vote.Session, tally vote.Tally) {
		passes.Add(1)
		seen = tally
	})

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-shrink", "u1", *voteSession.HookMessageID(), "⏭"))

	if passes.Load() != 1 {
		t.Fatalf("onPassed ran %d times, want 1 once the recomputed quorum dropped to 1", passes.Load())
	}
	if *seen.HookRequired() != 1 {
		t.Errorf("passing tally required = %d, want the recomputed 1 rather than the seeded 3", *seen.HookRequired())
	}
}

func TestAbsentRequesterCanConsentWithoutMovingTheQuorum(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"absentOwner"})

	discord := votingSession(t, "dispatch-consent", "u1", "u2", "u3")

	var passes atomic.Int32
	var seen vote.Tally
	voteSession := votingVote(t, "dispatch-consent", 2, func(_ *discordgo.Session, _ *vote.Session, tally vote.Tally) {
		passes.Add(1)
		seen = tally
	})

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-consent", "absentOwner", *voteSession.HookMessageID(), "⏭"))

	if passes.Load() != 1 {
		t.Fatalf("onPassed ran %d times, want 1 once the only requester consented", passes.Load())
	}
	if !*seen.HookByAdderConsent() {
		t.Error("the pass was not attributed to requester consent")
	}
	if *seen.HookCurrent() != 0 {
		t.Errorf("quorum count = %d, want 0 because the requester is not in the channel", *seen.HookCurrent())
	}
	if *seen.HookAdderVotes() != 1 || *seen.HookAdderTotal() != 1 {
		t.Errorf("requester counter = %d/%d, want 1/1", *seen.HookAdderVotes(), *seen.HookAdderTotal())
	}
}

func TestAbsentNonRequesterIsStillRejected(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"absentOwner"})

	discord := votingSession(t, "dispatch-outsider", "u1", "u2")
	voteSession := votingVote(t, "dispatch-outsider", 2, nil)

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-outsider", "randomAbsentee", *voteSession.HookMessageID(), "⏭"))

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
	voteSession := votingVote(t, "dispatch-two-owners", 3, func(*discordgo.Session, *vote.Session, vote.Tally) { passes.Add(1) })

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-two-owners", "ownerA", *voteSession.HookMessageID(), "⏭"))
	if passes.Load() != 0 {
		t.Fatal("the vote passed with only one of two requesters consenting")
	}

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-two-owners", "ownerB", *voteSession.HookMessageID(), "⏭"))
	if passes.Load() != 1 {
		t.Errorf("onPassed ran %d times after both requesters consented, want 1", passes.Load())
	}
}

func TestRequesterWithdrawalDropsConsent(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"ownerA", "ownerB"})

	discord := votingSession(t, "dispatch-withdraw-consent", "u1", "u2", "u3", "u4", "u5")

	var passes atomic.Int32
	voteSession := votingVote(t, "dispatch-withdraw-consent", 3, func(*discordgo.Session, *vote.Session, vote.Tally) { passes.Add(1) })

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-withdraw-consent", "ownerA", *voteSession.HookMessageID(), "⏭"))
	vote.HookOnVoteReactionRemove(discord, discordtest.ReactionRemove("dispatch-withdraw-consent", "ownerA", *voteSession.HookMessageID(), "⏭"))
	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-withdraw-consent", "ownerB", *voteSession.HookMessageID(), "⏭"))

	if passes.Load() != 0 {
		t.Error("the vote passed even though a requester withdrew their consent")
	}
	if len(*voteSession.HookAdderVotes()) != 1 {
		t.Errorf("adder votes = %d, want only ownerB", len(*voteSession.HookAdderVotes()))
	}
}

func TestPresentRequesterCountsForBothPaths(t *testing.T) {
	stubs := stubVoteEffects(t)
	stubs.useAdders([]string{"u2"})

	discord := votingSession(t, "dispatch-present-owner", "u1", "u2", "u3", "u4", "u5")

	var seen vote.Tally
	voteSession := votingVote(t, "dispatch-present-owner", 3, func(_ *discordgo.Session, _ *vote.Session, tally vote.Tally) {
		seen = tally
	})

	vote.HookOnVoteReactionAdd(discord, discordtest.ReactionAdd("dispatch-present-owner", "u2", *voteSession.HookMessageID(), "⏭"))

	if *seen.HookCurrent() != 1 {
		t.Errorf("quorum count = %d, want 1 because the requester is in the channel", *seen.HookCurrent())
	}
	if !*seen.HookByAdderConsent() {
		t.Error("the present requester's consent did not pass the vote")
	}
}
