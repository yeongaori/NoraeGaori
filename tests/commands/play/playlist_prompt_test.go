package play_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/commands/play"
	"noraegaori/internal/messages"
	"noraegaori/internal/youtube"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/discordtest"
)

const (
	promptReactionsPath = "/channels/chan1/messages/msg1/reactions"
	promptOriginalPath  = "/webhooks/" + discordtest.InteractionAppID + "/" + discordtest.InteractionToken + "/messages/@original"
)

func newPromptFixture(t *testing.T) (*discordgo.Session, func() []discordtest.Request, *discordgo.Message, *discordgo.InteractionCreate) {
	t.Helper()

	session, requests := discordtest.StubAPI(t, func(r *http.Request) int {
		if r.Method == http.MethodDelete && r.URL.Path == promptReactionsPath {
			return http.StatusForbidden
		}
		return http.StatusOK
	})
	testutil.Swap(t, play.HookPlaylistPromptExpiry, 10*time.Millisecond)

	msg := &discordgo.Message{ID: "msg1", ChannelID: "chan1"}
	interaction := discordtest.WithToken(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: "guild1",
			Member:  &discordgo.Member{User: &discordgo.User{ID: "requester"}},
		},
	})

	return session, requests, msg, interaction
}

func editedPromptEmbed(t *testing.T, requests []discordtest.Request) map[string]any {
	t.Helper()

	for index := range requests {
		if requests[index].Method == http.MethodPatch && requests[index].Path == promptOriginalPath {
			return discordtest.ReplyEmbed(t, &requests[index])
		}
	}
	t.Fatalf("got requests %v, want a PATCH to %s", requests, promptOriginalPath)
	return nil
}

func assertBotsReactionDropped(t *testing.T, requests []discordtest.Request, emoji string) {
	t.Helper()

	lines := make([]string, 0, len(requests))
	for i := range requests {
		request := &requests[i]
		lines = append(lines, request.Method+" "+request.Path)
	}
	want := "DELETE " + promptReactionsPath + "/" + emoji + "/@me"
	if !slices.Contains(lines, want) {
		t.Errorf("got requests %v, want %q among them", lines, want)
	}
}

func TestPlaylistRestPromptTimeoutKeepsTheSongAndDropsTheBotsArrow(t *testing.T) {
	session, requests, msg, interaction := newPromptFixture(t)
	timeoutEmbed := play.HookPlaylistTimeoutEmbed("guild1", timeoutSong(), false)

	play.HookHandlePlaylistRestConfirmationReaction(session, interaction, msg, "list1", "timeout", "", timeoutEmbed)()

	embed := editedPromptEmbed(t, requests())
	if want := messages.T("guild1").Titles.Added; embed["title"] != want {
		t.Errorf("got title %v, want %q", embed["title"], want)
	}
	if want := messages.T("guild1").Music.PlaylistTimeoutDesc; discordtest.JSONAt(t, embed, "footer", "text") != want {
		t.Errorf("got footer %v, want %q", embed["footer"], want)
	}
	assertBotsReactionDropped(t, requests(), "⬇️")
}

func TestPlaylistPromptTimeoutWarnsAndDropsTheBotsCheck(t *testing.T) {
	session, requests, msg, interaction := newPromptFixture(t)

	play.HookHandlePlaylistConfirmationReaction(session, interaction, msg, &youtube.PlaylistInfo{}, "")()

	embed := editedPromptEmbed(t, requests())
	if want := messages.T("guild1").Music.PlaylistTimeoutTitle; embed["title"] != want {
		t.Errorf("got title %v, want %q", embed["title"], want)
	}
	assertBotsReactionDropped(t, requests(), "✅")
}
