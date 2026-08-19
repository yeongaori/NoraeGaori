package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/youtube"
)

func newConfirmationFixture() (*discordgo.Session, *discordgo.Message, *discordgo.InteractionCreate) {
	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: "bot"}

	msg := &discordgo.Message{ID: "msg1", ChannelID: "chan1"}

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			GuildID: "guild1",
			Member:  &discordgo.Member{User: &discordgo.User{ID: "requester"}},
		},
	}

	return session, msg, interaction
}

func reactionFrom(userID, messageID, emoji string) *discordgo.MessageReactionAdd {
	return &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			UserID:    userID,
			MessageID: messageID,
			Emoji:     discordgo.Emoji{Name: emoji},
		},
	}
}

func TestConfirmedByRequesterAcceptsOnlyTheRequester(t *testing.T) {
	session, msg, interaction := newConfirmationFixture()

	removed := []string{}
	previous := removeUserReaction
	removeUserReaction = func(_ *discordgo.Session, _, _, _, userID string) {
		removed = append(removed, userID)
	}
	t.Cleanup(func() { removeUserReaction = previous })

	cases := []struct {
		name     string
		reaction *discordgo.MessageReactionAdd
		want     bool
	}{
		{"the requester on the prompt", reactionFrom("requester", "msg1", "⬇️"), true},
		{"the bot's own reaction", reactionFrom("bot", "msg1", "⬇️"), false},
		{"another message", reactionFrom("requester", "msg2", "⬇️"), false},
		{"another emoji", reactionFrom("requester", "msg1", "❤"), false},
		{"a bystander", reactionFrom("bystander", "msg1", "⬇️"), false},
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
