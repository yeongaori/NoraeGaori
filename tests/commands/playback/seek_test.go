package playback_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/playback"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestSeekDefersResponse(t *testing.T) {
	fixture := commandtest.NewFixture(t, []string{commandtest.CallerID}, queuetest.SongsBy(commandtest.CallerID, 1)...)
	interaction := discordtest.SlashInteraction(commandtest.GuildID, "seek", discordtest.Member(commandtest.GuildID, commandtest.CallerID),
		discordtest.StringOption("position", "0:30"))

	if err := playback.HandleSeek(fixture.Session, interaction); err != nil {
		t.Errorf("the handler returned %v, want nil", err)
	}

	requests := fixture.Requests()
	commandtest.WantReplyText(t, requests, messages.T(commandtest.GuildID).Music.NotPlayingOrLoading)
	if got, want := discordtest.ResponseType(&requests[0]), discordgo.InteractionResponseDeferredChannelMessageWithSource; got != want {
		t.Errorf("first response type = %d, want %d", got, want)
	}
	wantDeferredEdit(t, requests)
}

func TestSeekEditsSuccess(t *testing.T) {
	fixture := commandtest.NewFixture(t, []string{commandtest.CallerID}, queuetest.SongsBy(commandtest.CallerID, 1)...)
	player.GetPlayer(commandtest.GuildID).Playing = true
	t.Cleanup(func() { player.DeletePlayer(commandtest.GuildID) })
	interaction := discordtest.SlashInteraction(commandtest.GuildID, "seek", discordtest.Member(commandtest.GuildID, commandtest.CallerID),
		discordtest.StringOption("position", "0:30"))

	if err := playback.HandleSeek(fixture.Session, interaction); err != nil {
		t.Errorf("the handler returned %v, want nil", err)
	}

	requests := fixture.Requests()
	commandtest.WantReplyText(t, requests, messages.T(commandtest.GuildID).Music.SeekedTitle)
	wantDeferredEdit(t, requests)
}

func wantDeferredEdit(t *testing.T, requests []discordtest.Request) {
	t.Helper()

	last := commandtest.LastRequest(t, requests)
	if last.Method != http.MethodPatch || !strings.HasSuffix(last.Path, "/messages/@original") {
		t.Errorf("last request = %s %s, want an edit of the deferred reply", last.Method, last.Path)
	}
}

func TestParseSeekPosition(t *testing.T) {
	valid := []struct {
		in   string
		want int
	}{
		{"90", 90000},
		{"1:30", 90000},
		{"1:00:00", 3600000},
		{"0:00", 0},
		{"1:30.5", 90500},
		{"61", 61000},
	}
	for i := range valid {
		c := &valid[i]
		got, err := playback.HookParseSeekPosition(c.in)
		if err != nil {
			t.Errorf("parseSeekPosition(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSeekPosition(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	invalid := []string{"", "abc", "1:2:3:4", "1:60", "-5", "1:-5"}
	for _, in := range invalid {
		if _, err := playback.HookParseSeekPosition(in); err == nil {
			t.Errorf("parseSeekPosition(%q) should error", in)
		}
	}
}
