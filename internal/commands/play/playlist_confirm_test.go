package play

import (
	"noraegaori/internal/discord"
	"noraegaori/internal/testutil"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/testutil/discordtest"
	"noraegaori/internal/youtube"
)

func newConfirmationFixture(t *testing.T) (*discordgo.Session, *discordgo.Message, *discordgo.InteractionCreate) {
	session := discordtest.Session(t, "bot")

	msg := &discordgo.Message{ID: "msg1", ChannelID: "chan1"}

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			GuildID: "guild1",
			Member:  &discordgo.Member{User: &discordgo.User{ID: "requester"}},
		},
	}

	return session, msg, interaction
}

func TestConfirmedByRequesterAcceptsOnlyTheRequester(t *testing.T) {
	session, msg, interaction := newConfirmationFixture(t)

	removed := []string{}
	testutil.Swap(t, &discord.RemoveUserReaction, func(_ *discordgo.Session, _, _, _, userID string) {
		removed = append(removed, userID)
	})

	cases := []struct {
		name     string
		reaction *discordgo.MessageReactionAdd
		want     bool
	}{
		{"the requester on the prompt", discordtest.ReactionAdd("", "requester", "msg1", "⬇️"), true},
		{"the bot's own reaction", discordtest.ReactionAdd("", "bot", "msg1", "⬇️"), false},
		{"another message", discordtest.ReactionAdd("", "requester", "msg2", "⬇️"), false},
		{"another emoji", discordtest.ReactionAdd("", "requester", "msg1", "❤"), false},
		{"a bystander", discordtest.ReactionAdd("", "bystander", "msg1", "⬇️"), false},
	}

	for _, testCase := range cases {
		got := confirmedByRequester(session, testCase.reaction, msg, interaction)
		if got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}

	if len(removed) != 1 || removed[0] != "bystander" {
		t.Errorf("got removed reactions %v, want only the bystander's stripped", removed)
	}
}

func TestExcludeVideoDropsOnlyTheNamedVideo(t *testing.T) {
	videos := []*youtube.Song{
		{URL: "https://www.youtube.com/watch?v=keepme1", Title: "First"},
		{URL: "https://www.youtube.com/watch?v=dropme", Title: "Excluded"},
		{URL: "https://www.youtube.com/watch?v=keepme2", Title: "Second"},
	}

	remaining := excludeVideo(videos, "dropme")
	if len(remaining) != 2 {
		t.Fatalf("got %d videos, want 2", len(remaining))
	}
	for _, video := range remaining {
		if video.Title == "Excluded" {
			t.Error("the excluded video survived the filter")
		}
	}
}

func TestExcludeVideoKeepsEverythingWithoutAnID(t *testing.T) {
	videos := []*youtube.Song{
		{URL: "https://www.youtube.com/watch?v=a"},
		{URL: "https://www.youtube.com/watch?v=b"},
	}

	if got := excludeVideo(videos, ""); len(got) != 2 {
		t.Errorf("got %d videos, want all 2 kept when no video is excluded", len(got))
	}
}
