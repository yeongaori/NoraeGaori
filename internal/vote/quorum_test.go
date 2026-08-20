package vote

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func voiceStateWithMember(userID, channelID string, isBot bool) *discordgo.VoiceState {
	return &discordgo.VoiceState{
		GuildID:   "g",
		UserID:    userID,
		ChannelID: channelID,
		Member:    &discordgo.Member{GuildID: "g", User: &discordgo.User{ID: userID, Bot: isBot}},
	}
}

func sessionWithGuild(t *testing.T, states []*discordgo.VoiceState, members []*discordgo.Member) *discordgo.Session {
	t.Helper()

	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "self"}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "g", VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	for _, member := range members {
		if err := session.State.MemberAdd(member); err != nil {
			t.Fatalf("failed to seed member %s: %v", member.User.ID, err)
		}
	}
	return session
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
		session := sessionWithGuild(t, testCase.states, testCase.members)

		got, err := RequiredInChannel(session, "g", "voice", ResolveWithFetch)
		if err != nil {
			t.Fatalf("%s: RequiredInChannel returned %v, want nil", testCase.name, err)
		}
		if got != testCase.want {
			t.Errorf("%s: got %d required votes, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestRequiredVotesInChannelCountsMembersAttachedToVoiceStates(t *testing.T) {
	states := []*discordgo.VoiceState{
		voiceStateWithMember("u1", "voice", false),
		voiceStateWithMember("u2", "voice", false),
		voiceStateWithMember("u3", "voice", false),
		voiceStateWithMember("musicbot", "voice", true),
	}

	session := sessionWithGuild(t, states, nil)

	for _, mode := range []memberResolution{resolveFromCache, ResolveWithFetch} {
		got, err := RequiredInChannel(session, "g", "voice", mode)
		if err != nil {
			t.Fatalf("RequiredInChannel returned %v, want nil", err)
		}
		if got != 2 {
			t.Errorf("mode %v: got %d required votes for three humans and a bot, want 2", mode, got)
		}
	}
}

func TestRequiredVotesInChannelSkipsTheBotItself(t *testing.T) {
	states := []*discordgo.VoiceState{
		voiceStateWithMember("u1", "voice", false),
		{GuildID: "g", UserID: "self", ChannelID: "voice"},
	}

	session := sessionWithGuild(t, states, nil)

	got, err := RequiredInChannel(session, "g", "voice", resolveFromCache)
	if err != nil {
		t.Fatalf("RequiredInChannel returned %v, want nil", err)
	}
	if got != 1 {
		t.Errorf("got %d required votes, want 1 with only the bot beside one listener", got)
	}
}

func TestRequiredVotesInChannelCountsUnresolvedMembersOnTheHotPath(t *testing.T) {
	states := []*discordgo.VoiceState{
		{GuildID: "g", UserID: "u1", ChannelID: "voice"},
		{GuildID: "g", UserID: "u2", ChannelID: "voice"},
		{GuildID: "g", UserID: "u3", ChannelID: "voice"},
	}

	session := sessionWithGuild(t, states, nil)

	cached, err := RequiredInChannel(session, "g", "voice", resolveFromCache)
	if err != nil {
		t.Fatalf("RequiredInChannel returned %v, want nil", err)
	}
	if cached != 2 {
		t.Errorf("cache mode counted %d required votes, want 2 treating unresolved members as human", cached)
	}
}

func TestRequiredVotesInChannelReportsAnUnknownGuild(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}

	if _, err := RequiredInChannel(session, "missing", "voice", resolveFromCache); err == nil {
		t.Error("got nil error, want a failure for a guild that is not cached")
	}
}

func TestClassifyVoterSortsPresentAbsentAndUnknown(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "self"}

	if _, ok := classifyVoter(session, "guild1", "voice1", "stranger", nil); ok {
		t.Error("a user with no voice state was accepted")
	}

	guild := &discordgo.Guild{
		ID: "guild1",
		VoiceStates: []*discordgo.VoiceState{
			{GuildID: "guild1", UserID: "human", ChannelID: "voice1"},
			{GuildID: "guild1", UserID: "elsewhere", ChannelID: "voice2"},
			{GuildID: "guild1", UserID: "botuser", ChannelID: "voice1"},
			{
				GuildID:   "guild1",
				UserID:    "attached",
				ChannelID: "voice1",
				Member:    &discordgo.Member{GuildID: "guild1", User: &discordgo.User{ID: "attached"}},
			},
		},
	}
	if err := session.State.GuildAdd(guild); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}

	members := []*discordgo.Member{
		{GuildID: "guild1", User: &discordgo.User{ID: "botuser", Bot: true}},
		{GuildID: "guild1", User: &discordgo.User{ID: "human"}},
		{GuildID: "guild1", User: &discordgo.User{ID: "elsewhere"}},
		{GuildID: "guild1", User: &discordgo.User{ID: "absentBot", Bot: true}},
	}
	for _, member := range members {
		if err := session.State.MemberAdd(member); err != nil {
			t.Fatalf("failed to seed member %s: %v", member.User.ID, err)
		}
	}

	if _, ok := classifyVoter(session, "guild1", "voice1", "botuser", nil); ok {
		t.Error("a bot in the channel was accepted as a voter")
	}
	if _, ok := classifyVoter(session, "guild1", "voice1", "elsewhere", nil); ok {
		t.Error("a member in a different voice channel was accepted")
	}

	ballot, ok := classifyVoter(session, "guild1", "voice1", "human", nil)
	if !ok || !ballot.countsFor || ballot.isAdder {
		t.Errorf("present non-adder = %+v ok=%v, want a quorum-counting ballot", ballot, ok)
	}

	ballot, ok = classifyVoter(session, "guild1", "voice1", "attached", []string{"attached"})
	if !ok || !ballot.countsFor || !ballot.isAdder {
		t.Errorf("present adder = %+v ok=%v, want both flags set", ballot, ok)
	}
}

func TestClassifyVoterLetsAbsentAddersConsent(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "self"}

	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild1"}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	if err := session.State.MemberAdd(&discordgo.Member{GuildID: "guild1", User: &discordgo.User{ID: "absentBot", Bot: true}}); err != nil {
		t.Fatalf("failed to seed the bot member: %v", err)
	}

	ballot, ok := classifyVoter(session, "guild1", "voice1", "absentOwner", []string{"absentOwner"})
	if !ok {
		t.Fatal("an absent requester was refused a consent vote")
	}
	if ballot.countsFor {
		t.Error("an absent requester's ballot counts toward the quorum")
	}
	if !ballot.isAdder {
		t.Error("an absent requester's ballot was not marked as consent")
	}

	if _, ok := classifyVoter(session, "guild1", "voice1", "absentOther", []string{"absentOwner"}); ok {
		t.Error("an absent non-requester was accepted")
	}
	if _, ok := classifyVoter(session, "guild1", "voice1", "absentBot", []string{"absentBot"}); ok {
		t.Error("an absent bot in the adder set was accepted")
	}
	if _, ok := classifyVoter(session, "guild1", "voice1", "self", []string{"self"}); ok {
		t.Error("the bot itself was accepted as an absent adder")
	}
}
