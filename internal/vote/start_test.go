package vote

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/testutil/discordtest"
)

const originalReplyPath = "/webhooks/app/token/messages/@original"

func startInteraction(guildID string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:      discordtest.InteractionID,
			AppID:   discordtest.InteractionAppID,
			Token:   discordtest.InteractionToken,
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: guildID,
			Member:  &discordgo.Member{User: &discordgo.User{ID: "voter"}},
		},
	}
}

func startRequest() Request {
	return Request{Kind: KindSkip, Title: "Skip vote", Description: "Skip the song?", Emoji: "⏭", VoiceChannelID: "voice1", RequiredVotes: 2}
}

func waitUntil(t *testing.T, condition func() bool, failure string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(failure)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (v *voteStubs) addedMessages() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.added...)
}

func requestPaths(sent []discordtest.Request) []string {
	paths := make([]string, 0, len(sent))
	for index := range sent {
		paths = append(paths, sent[index].Method+" "+sent[index].Path)
	}
	return paths
}

func TestStartPostsTheVoteAndWaitsForBallots(t *testing.T) {
	const guildID = "start-guild"
	stubs := stubVoteEffects(t)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	if err := Start(session, startInteraction(guildID), startRequest()); err != nil {
		t.Fatalf("Start returned %v", err)
	}

	want := []string{http.MethodPatch + " " + originalReplyPath, http.MethodGet + " " + originalReplyPath}
	if got := requestPaths(requests()); !slices.Equal(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
	if snapshot, isLive := activeVotes.snapshotOf(guildID, KindSkip); !isLive || snapshot.messageID != "333" {
		t.Errorf("vote snapshot = (%+v, %v), want a live vote on message 333", snapshot, isLive)
	}
	waitUntil(t, func() bool { return slices.Contains(stubs.addedMessages(), "333") }, "the vote prompt reaction was never added")

	activeVotes.cancel(guildID, voteEndCancelled, KindSkip)
	waitUntil(t, func() bool { return slices.Contains(stubs.clearedMessages(), "333") }, "the vote did not finish after being cancelled")
}

func TestStartRefusesASecondVoteOfTheSameKind(t *testing.T) {
	const guildID = "start-busy-guild"
	stubVoteEffects(t)
	liveVote(t, guildID, KindSkip)
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))

	if err := Start(session, startInteraction(guildID), startRequest()); err != nil {
		t.Fatalf("Start returned %v", err)
	}

	sent := requests()
	if len(sent) != 1 || sent[0].Method != http.MethodPatch || sent[0].Path != originalReplyPath {
		t.Fatalf("sent %v, want one edit of the original reply", requestPaths(sent))
	}
	body := string(sent[0].RawBody)
	for _, want := range []string{messages.T(guildID).Votes.InProgress, voteMessageURL(guildID, "chan-"+guildID, "msg-"+guildID)} {
		if !strings.Contains(body, want) {
			t.Errorf("the in-progress reply %s does not contain %q", body, want)
		}
	}
	if snapshot, isLive := activeVotes.snapshotOf(guildID, KindSkip); !isLive || snapshot.messageID != "msg-"+guildID {
		t.Errorf("the running vote was replaced: %+v", snapshot)
	}
}

func TestStartReleasesTheVoteWhenItsMessageCannotBeFound(t *testing.T) {
	const guildID = "start-lost-guild"
	stubVoteEffects(t)
	session, requests := discordtest.StubAPI(t, func(r *http.Request) int {
		if r.Method == http.MethodGet {
			return http.StatusNotFound
		}
		return http.StatusOK
	})

	if err := Start(session, startInteraction(guildID), startRequest()); err != nil {
		t.Fatalf("Start returned %v", err)
	}

	sent := requests()
	if len(sent) != 3 || !strings.Contains(string(sent[2].RawBody), messages.T(guildID).Errors.CommandExecutionError) {
		t.Errorf("sent %v, want the progress, the failed lookup and an error reply", requestPaths(sent))
	}
	if _, isLive := activeVotes.snapshotOf(guildID, KindSkip); isLive {
		t.Error("the vote stayed claimed after its message could not be found")
	}
}

func TestRequiredVotesFetchMembersMissingFromTheCache(t *testing.T) {
	const guildID = "fetch-guild"
	session, requests := discordtest.StubAPI(t, discordtest.Status(http.StatusOK))
	session.State.User = &discordgo.User{ID: "bot"}
	discordtest.AddGuild(t, session, guildID, &discordgo.VoiceState{GuildID: guildID, UserID: "uncached", ChannelID: "voice1"})

	required, err := RequiredInChannel(session, guildID, "voice1", ResolveWithFetch)
	if err != nil || required != 1 {
		t.Fatalf("RequiredInChannel = (%d, %v), want 1 vote", required, err)
	}

	if sent := requests(); len(sent) != 1 || sent[0].Path != "/guilds/"+guildID+"/members/uncached" {
		t.Errorf("sent %v, want one member lookup", requestPaths(sent))
	}
	if _, err := session.State.Member(guildID, "fetched"); err != nil {
		t.Errorf("the fetched member was not cached: %v", err)
	}
}
