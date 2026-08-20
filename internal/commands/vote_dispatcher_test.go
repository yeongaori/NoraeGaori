package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func dispatcherSession(t *testing.T) *discordgo.Session {
	t.Helper()

	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "bot"}
	return session
}

func TestVoteForReactionRoutesOnlyItsOwnMessage(t *testing.T) {
	session := dispatcherSession(t)

	vote := newVoteSession("guild1", voteKindSkip, "Skip", "⏭", "voice1", 2)
	if _, claimed := activeVotes.claim(vote); !claimed {
		t.Fatal("failed to seed the vote")
	}
	t.Cleanup(func() { activeVotes.release(vote) })
	activeVotes.attachMessage(vote, "msg1", "chan1")

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
		got := voteForReaction(session, testCase.userID, testCase.messageID, testCase.emoji) != nil
		if got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestVoteForReactionSurvivesAnEmptyState(t *testing.T) {
	if voteForReaction(&discordgo.Session{}, "u1", "msg1", "⏭") != nil {
		t.Error("a session with no state routed a reaction")
	}

	stateless := &discordgo.Session{State: discordgo.NewState()}
	if voteForReaction(stateless, "u1", "msg1", "⏭") != nil {
		t.Error("a session with no cached user routed a reaction")
	}
}

func TestCurrentThresholdFallsBackToTheSeedValue(t *testing.T) {
	stubVoteEffects(t)
	session := dispatcherSession(t)

	vote := newVoteSession("missing-guild", voteKindSkip, "Skip", "⏭", "voice1", 4)

	if got := currentThreshold(session, vote); got.quorum != 4 {
		t.Errorf("currentThreshold on an uncached guild = %d, want the seed value 4", got.quorum)
	}
}

func TestCurrentThresholdRecomputesFromTheChannel(t *testing.T) {
	stubVoteEffects(t)
	session := dispatcherSession(t)

	states := []*discordgo.VoiceState{
		voiceStateWithMember("u1", "voice1", false),
		voiceStateWithMember("u2", "voice1", false),
		voiceStateWithMember("u3", "voice1", false),
	}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "quorum-guild", VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}

	vote := newVoteSession("quorum-guild", voteKindSkip, "Skip", "⏭", "voice1", 9)

	if got := currentThreshold(session, vote); got.quorum != 2 {
		t.Errorf("currentThreshold = %d, want 2 recomputed from the three listeners", got.quorum)
	}
}
