package vote_test

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/vote"
	"noraegaori/tests/testutil/discordtest"
)

func TestVoteForReactionRoutesOnlyItsOwnMessage(t *testing.T) {
	session := discordtest.Session(t, "bot")

	voteSession := vote.HookNewVoteSession("guild1", vote.KindSkip, "Skip", "⏭", "voice1", 2)
	if _, claimed := (*vote.HookActiveVotes).HookClaim(voteSession); !claimed {
		t.Fatal("failed to seed the vote")
	}
	t.Cleanup(func() { (*vote.HookActiveVotes).HookRelease(voteSession) })
	(*vote.HookActiveVotes).HookAttachMessage(voteSession, "msg1", "chan1")

	cases := []struct {
		name      string
		userID    string
		messageID string
		emoji     string
		want      bool
	}{
		{"a real voter on the vote message", "u1", "msg1", "⏭", true},
		{"the bot's own reaction", "bot", "msg1", "⏭", false},
		{"a reaction on another message", "u1", "msg2", "⏭", false},
		{"a different emoji", "u1", "msg1", "❤", false},
	}

	for _, testCase := range cases {
		got := vote.HookVoteForReaction(session, testCase.userID, testCase.messageID, testCase.emoji) != nil
		if got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestVoteForReactionSurvivesAnEmptyState(t *testing.T) {
	if vote.HookVoteForReaction(&discordgo.Session{}, "u1", "msg1", "⏭") != nil {
		t.Error("a session with no state routed a reaction")
	}

	stateless := &discordgo.Session{State: discordgo.NewState()}
	if vote.HookVoteForReaction(stateless, "u1", "msg1", "⏭") != nil {
		t.Error("a session with no cached user routed a reaction")
	}
}

func TestCurrentThresholdFallsBackToTheSeedValue(t *testing.T) {
	stubVoteEffects(t)
	session := discordtest.Session(t, "bot")

	voteSession := vote.HookNewVoteSession("missing-guild", vote.KindSkip, "Skip", "⏭", "voice1", 4)

	if got := vote.HookCurrentThreshold(session, voteSession); *got.HookQuorum() != 4 {
		t.Errorf("currentThreshold on an uncached guild = %d, want the seed value 4", *got.HookQuorum())
	}
}

func TestCurrentThresholdRecomputesFromTheChannel(t *testing.T) {
	stubVoteEffects(t)
	session := discordtest.Session(t, "bot")

	states := []*discordgo.VoiceState{
		voiceStateWithMember("u1", "voice1", false),
		voiceStateWithMember("u2", "voice1", false),
		voiceStateWithMember("u3", "voice1", false),
	}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "quorum-guild", VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}

	voteSession := vote.HookNewVoteSession("quorum-guild", vote.KindSkip, "Skip", "⏭", "voice1", 9)

	if got := vote.HookCurrentThreshold(session, voteSession); *got.HookQuorum() != 2 {
		t.Errorf("currentThreshold = %d, want 2 recomputed from the three listeners", *got.HookQuorum())
	}
}
