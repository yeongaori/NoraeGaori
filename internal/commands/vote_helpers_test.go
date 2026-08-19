package commands

import (
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func newTestVoteSession() *voteSession {
	return &voteSession{
		votes:         make(map[string]bool),
		requiredVotes: 3,
	}
}

func TestCastVote(t *testing.T) {
	vs := newTestVoteSession()

	if count, counted := vs.castVote("u1"); count != 1 || !counted {
		t.Errorf("castVote(u1) = (%d, %v), want (1, true)", count, counted)
	}
	if count, counted := vs.castVote("u1"); count != 1 || counted {
		t.Errorf("castVote(u1) repeated = (%d, %v), want (1, false)", count, counted)
	}
	if count, counted := vs.castVote("u2"); count != 2 || !counted {
		t.Errorf("castVote(u2) = (%d, %v), want (2, true)", count, counted)
	}
}

func TestWithdrawVote(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote("u1")
	vs.castVote("u2")

	if count, withdrawn := vs.withdrawVote("u3"); count != 2 || withdrawn {
		t.Errorf("withdrawVote(u3) = (%d, %v), want (2, false)", count, withdrawn)
	}
	if count, withdrawn := vs.withdrawVote("u2"); count != 1 || !withdrawn {
		t.Errorf("withdrawVote(u2) = (%d, %v), want (1, true)", count, withdrawn)
	}
	if count, withdrawn := vs.withdrawVote("u2"); count != 1 || withdrawn {
		t.Errorf("withdrawVote(u2) repeated = (%d, %v), want (1, false)", count, withdrawn)
	}
	if count, withdrawn := vs.withdrawVote("u1"); count != 0 || !withdrawn {
		t.Errorf("withdrawVote(u1) = (%d, %v), want (0, true)", count, withdrawn)
	}
}

func TestVoteMessageURL(t *testing.T) {
	want := "https://discord.com/channels/g1/c1/m1"
	if got := voteMessageURL("g1", "c1", "m1"); got != want {
		t.Errorf("voteMessageURL = %q, want %q", got, want)
	}
}

func TestClaimVoteSession(t *testing.T) {
	votes := make(map[string]*voteSession)
	var mutex sync.RWMutex

	first := newTestVoteSession()
	if existing := claimVoteSession(votes, &mutex, "g1", first); existing != nil {
		t.Errorf("claimVoteSession on empty map = %v, want nil", existing)
	}
	if votes["g1"] != first {
		t.Error("claimVoteSession did not store the session")
	}

	second := newTestVoteSession()
	if existing := claimVoteSession(votes, &mutex, "g1", second); existing != first {
		t.Errorf("claimVoteSession on claimed guild = %v, want the first session", existing)
	}
	if votes["g1"] != first {
		t.Error("claimVoteSession overwrote the live session")
	}

	if existing := claimVoteSession(votes, &mutex, "g2", second); existing != nil {
		t.Errorf("claimVoteSession for another guild = %v, want nil", existing)
	}
}

func TestActiveVoteForAndRelease(t *testing.T) {
	votes := make(map[string]*voteSession)
	var mutex sync.RWMutex

	if got := activeVoteFor(votes, &mutex, "g1"); got != nil {
		t.Errorf("activeVoteFor on empty map = %v, want nil", got)
	}

	session := newTestVoteSession()
	claimVoteSession(votes, &mutex, "g1", session)
	if got := activeVoteFor(votes, &mutex, "g1"); got != session {
		t.Errorf("activeVoteFor = %v, want the claimed session", got)
	}

	releaseVoteSession(votes, &mutex, "g1", newTestVoteSession())
	if got := activeVoteFor(votes, &mutex, "g1"); got != session {
		t.Error("releaseVoteSession removed a session it does not own")
	}

	releaseVoteSession(votes, &mutex, "g1", session)
	if got := activeVoteFor(votes, &mutex, "g1"); got != nil {
		t.Errorf("activeVoteFor after release = %v, want nil", got)
	}
}

func TestCastVoteAfterWithdraw(t *testing.T) {
	vs := newTestVoteSession()
	vs.castVote("u1")
	vs.withdrawVote("u1")

	if count, counted := vs.castVote("u1"); count != 1 || !counted {
		t.Errorf("castVote(u1) after withdraw = (%d, %v), want (1, true)", count, counted)
	}
}

func newTestVoteReaction() (*voteReaction, *discordgo.Session) {
	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "bot"}

	vs := newTestVoteSession()
	vs.messageID = "msg1"
	vs.channelID = "chan1"
	vs.voiceChannelID = "voice1"

	return &voteReaction{
		guildID:    "guild1",
		title:      "Skip",
		emoji:      "⏭",
		session:    vs,
		votesMap:   map[string]*voteSession{"guild1": vs},
		votesMutex: &sync.RWMutex{},
		voteDone:   make(chan bool, 1),
	}, session
}

func TestTargetsThisVote(t *testing.T) {
	vote, session := newTestVoteReaction()

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
		got := vote.targetsThisVote(session, testCase.userID, testCase.messageID, testCase.emoji)
		if got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestVoterIsEligibleRejectsUnknownAndAbsentMembers(t *testing.T) {
	vote, session := newTestVoteReaction()

	if vote.voterIsEligible(session, "stranger") {
		t.Error("a user with no cached member record was accepted")
	}

	guild := &discordgo.Guild{
		ID: "guild1",
		VoiceStates: []*discordgo.VoiceState{
			{GuildID: "guild1", UserID: "human", ChannelID: "voice1"},
			{GuildID: "guild1", UserID: "elsewhere", ChannelID: "voice2"},
			{GuildID: "guild1", UserID: "botuser", ChannelID: "voice1"},
		},
	}
	if err := session.State.GuildAdd(guild); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}

	members := []*discordgo.Member{
		{GuildID: "guild1", User: &discordgo.User{ID: "botuser", Bot: true}},
		{GuildID: "guild1", User: &discordgo.User{ID: "human"}},
		{GuildID: "guild1", User: &discordgo.User{ID: "elsewhere"}},
	}
	for _, member := range members {
		if err := session.State.MemberAdd(member); err != nil {
			t.Fatalf("failed to seed member %s: %v", member.User.ID, err)
		}
	}

	if vote.voterIsEligible(session, "botuser") {
		t.Error("a bot was accepted as a voter")
	}
	if vote.voterIsEligible(session, "elsewhere") {
		t.Error("a member in a different voice channel was accepted")
	}
	if !vote.voterIsEligible(session, "human") {
		t.Error("a member in the vote's voice channel was rejected")
	}
}

func TestRequiredVotesInChannelIgnoresBotsAndOtherChannels(t *testing.T) {
	cases := []struct {
		name    string
		members []*discordgo.Member
		states  []*discordgo.VoiceState
		want    int
	}{
		{
			name:    "an empty channel still needs one vote",
			members: nil,
			states:  nil,
			want:    1,
		},
		{
			name:    "a lone listener needs one vote",
			members: []*discordgo.Member{{GuildID: "g", User: &discordgo.User{ID: "u1"}}},
			states:  []*discordgo.VoiceState{{GuildID: "g", UserID: "u1", ChannelID: "voice"}},
			want:    1,
		},
		{
			name: "four listeners need two votes",
			members: []*discordgo.Member{
				{GuildID: "g", User: &discordgo.User{ID: "u1"}},
				{GuildID: "g", User: &discordgo.User{ID: "u2"}},
				{GuildID: "g", User: &discordgo.User{ID: "u3"}},
				{GuildID: "g", User: &discordgo.User{ID: "u4"}},
			},
			states: []*discordgo.VoiceState{
				{GuildID: "g", UserID: "u1", ChannelID: "voice"},
				{GuildID: "g", UserID: "u2", ChannelID: "voice"},
				{GuildID: "g", UserID: "u3", ChannelID: "voice"},
				{GuildID: "g", UserID: "u4", ChannelID: "voice"},
			},
			want: 2,
		},
		{
			name: "three listeners round up to two votes",
			members: []*discordgo.Member{
				{GuildID: "g", User: &discordgo.User{ID: "u1"}},
				{GuildID: "g", User: &discordgo.User{ID: "u2"}},
				{GuildID: "g", User: &discordgo.User{ID: "u3"}},
			},
			states: []*discordgo.VoiceState{
				{GuildID: "g", UserID: "u1", ChannelID: "voice"},
				{GuildID: "g", UserID: "u2", ChannelID: "voice"},
				{GuildID: "g", UserID: "u3", ChannelID: "voice"},
			},
			want: 2,
		},
		{
			name: "bots and other channels do not count",
			members: []*discordgo.Member{
				{GuildID: "g", User: &discordgo.User{ID: "u1"}},
				{GuildID: "g", User: &discordgo.User{ID: "u2"}},
				{GuildID: "g", User: &discordgo.User{ID: "bot1", Bot: true}},
				{GuildID: "g", User: &discordgo.User{ID: "bot2", Bot: true}},
				{GuildID: "g", User: &discordgo.User{ID: "elsewhere"}},
			},
			states: []*discordgo.VoiceState{
				{GuildID: "g", UserID: "u1", ChannelID: "voice"},
				{GuildID: "g", UserID: "u2", ChannelID: "voice"},
				{GuildID: "g", UserID: "bot1", ChannelID: "voice"},
				{GuildID: "g", UserID: "bot2", ChannelID: "voice"},
				{GuildID: "g", UserID: "elsewhere", ChannelID: "other"},
			},
			want: 1,
		},
	}

	for _, testCase := range cases {
		session := &discordgo.Session{State: discordgo.NewState()}
		if err := session.State.GuildAdd(&discordgo.Guild{ID: "g", VoiceStates: testCase.states}); err != nil {
			t.Fatalf("%s: failed to seed the guild: %v", testCase.name, err)
		}
		for _, member := range testCase.members {
			if err := session.State.MemberAdd(member); err != nil {
				t.Fatalf("%s: failed to seed member %s: %v", testCase.name, member.User.ID, err)
			}
		}

		got, err := requiredVotesInChannel(session, "g", "voice")
		if err != nil {
			t.Fatalf("%s: requiredVotesInChannel returned %v, want nil", testCase.name, err)
		}
		if got != testCase.want {
			t.Errorf("%s: got %d required votes, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestRequiredVotesInChannelReportsAnUnknownGuild(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}

	if _, err := requiredVotesInChannel(session, "missing", "voice"); err == nil {
		t.Error("got nil error, want a failure for a guild that is not cached")
	}
}
