package discord_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/tests/testutil/discordtest"
)

const reactionsPath = "/channels/chan1/messages/msg1/reactions"

func requestLines(requests []discordtest.Request) []string {
	lines := make([]string, 0, len(requests))
	for _, request := range requests {
		lines = append(lines, request.Method+" "+request.Path)
	}
	return lines
}

func cachePromptChannel(t *testing.T, session *discordgo.Session, permissions int64) {
	t.Helper()

	session.State.User = &discordgo.User{ID: "bot"}
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID:      "guild1",
		OwnerID: "owner",
		Roles:   []*discordgo.Role{{ID: "guild1", Permissions: permissions}},
	}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	if err := session.State.ChannelAdd(&discordgo.Channel{ID: "chan1", GuildID: "guild1"}); err != nil {
		t.Fatalf("failed to seed the channel: %v", err)
	}
	if err := session.State.MemberAdd(&discordgo.Member{GuildID: "guild1", User: &discordgo.User{ID: "bot"}}); err != nil {
		t.Fatalf("failed to seed the bot member: %v", err)
	}
}

func TestClearPromptReactionsSkipsTheClearWithoutManageMessages(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusNoContent))
	cachePromptChannel(t, session, discordgo.PermissionAddReactions|discordgo.PermissionSendMessages)

	discord.ClearPromptReactions(session, "chan1", "msg1", "⬇️")

	got := requestLines(requests())
	want := []string{"DELETE " + reactionsPath + "/⬇️/@me"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got requests %v, want %v", got, want)
	}
}

func TestClearPromptReactionsClearsEverythingWithManageMessages(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusNoContent))
	cachePromptChannel(t, session, discordgo.PermissionAddReactions|discordgo.PermissionManageMessages)

	discord.ClearPromptReactions(session, "chan1", "msg1", "⬇️")

	got := requestLines(requests())
	want := []string{"DELETE " + reactionsPath}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got requests %v, want %v", got, want)
	}
}

func TestClearPromptReactionsTriesWhenTheChannelIsUncached(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusNoContent))
	session.State.User = &discordgo.User{ID: "bot"}

	discord.ClearPromptReactions(session, "chan1", "msg1", "⬇️")

	got := requestLines(requests())
	want := []string{"DELETE " + reactionsPath}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got requests %v, want %v", got, want)
	}
}

func TestRemoveUserReactionKeepsABystandersReactionWithoutManageMessages(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusNoContent))
	cachePromptChannel(t, session, discordgo.PermissionAddReactions|discordgo.PermissionSendMessages)

	discord.RemoveUserReaction(session, "chan1", "msg1", "⬇️", "bystander")

	if got := requestLines(requests()); len(got) != 0 {
		t.Errorf("got requests %v, want %v", got, []string{})
	}
}

func TestRemoveUserReactionRemovesABystandersReactionWithManageMessages(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusNoContent))
	cachePromptChannel(t, session, discordgo.PermissionAddReactions|discordgo.PermissionManageMessages)

	discord.RemoveUserReaction(session, "chan1", "msg1", "⬇️", "bystander")

	got := requestLines(requests())
	want := []string{"DELETE " + reactionsPath + "/⬇️/bystander"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got requests %v, want %v", got, want)
	}
}

func TestClearPromptReactionsFallsBackToTheBotsOwnReaction(t *testing.T) {
	session, requests := discordtest.StubAPI(t, func(r *http.Request) int {
		if r.URL.Path == reactionsPath {
			return http.StatusForbidden
		}
		return http.StatusNoContent
	})

	discord.ClearPromptReactions(session, "chan1", "msg1", "⬇️")

	got := requestLines(requests())
	want := []string{"DELETE " + reactionsPath, "DELETE " + reactionsPath + "/⬇️/@me"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got requests %v, want %v", got, want)
	}
}

func TestClearPromptReactionsLeavesTheBotsReactionAloneWhenAllAreCleared(t *testing.T) {
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusNoContent))

	discord.ClearPromptReactions(session, "chan1", "msg1", "⬇️")

	got := requestLines(requests())
	want := []string{"DELETE " + reactionsPath}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got requests %v, want %v", got, want)
	}
}
