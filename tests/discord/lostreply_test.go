package discord_test

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/discord"
	"noraegaori/tests/testutil/discordtest"
)

const recentReplies = `[
	{"id":"900","channel_id":"c","author":{"id":"bot"},"message_reference":{"message_id":"orig"}},
	{"id":"1000","channel_id":"c","author":{"id":"bot"},"message_reference":{"message_id":"orig"}},
	{"id":"1100","channel_id":"c","author":{"id":"user"},"message_reference":{"message_id":"orig"}},
	{"id":"1200","channel_id":"c","author":{"id":"bot"}},
	{"id":"1300","channel_id":"c","author":{"id":"bot"},"message_reference":{"message_id":"other"}}
]`

const lookupFails = ""

type replyStub struct {
	session     *discordgo.Session
	interaction *discordgo.InteractionCreate
	responder   *discord.MessageResponse
	requests    func() []discordtest.Request
}

func stubReplies(t *testing.T, postStatuses []int, recent string) *replyStub {
	t.Helper()

	var posts atomic.Int32
	session, requests := discordtest.StubAPIResponder(t, func(r *http.Request) (int, string) {
		switch r.Method {
		case http.MethodPost:
			index := int(posts.Add(1)) - 1
			if index < len(postStatuses) && postStatuses[index] != http.StatusOK {
				return postStatuses[index], `{}`
			}
			return http.StatusOK, `{"id":"901","channel_id":"c"}`
		case http.MethodGet:
			if recent == lookupFails {
				return http.StatusForbidden, `{}`
			}
			return http.StatusOK, recent
		}
		return http.StatusOK, `{"id":"1000","channel_id":"c"}`
	})
	session.State.User = &discordgo.User{ID: "bot"}

	stub := &replyStub{
		session:     session,
		interaction: &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Token: "message_orig_c", ChannelID: "c"}},
		responder:   &discord.MessageResponse{Session: session, ChannelID: "c", OriginalMsgID: "orig"},
		requests:    requests,
	}
	t.Cleanup(discord.RegisterResponder(stub.interaction.Token, stub.responder))
	return stub
}

func (stub *replyStub) count(method, path string) int {
	count := 0
	for _, request := range stub.requests() {
		if request.Method == method && (path == "" || request.Path == path) {
			count++
		}
	}
	return count
}

func (stub *replyStub) lastPatch(t *testing.T) *discordtest.Request {
	t.Helper()

	requests := stub.requests()
	for i := len(requests) - 1; i >= 0; i-- {
		if requests[i].Method == http.MethodPatch {
			return &requests[i]
		}
	}
	t.Fatalf("got requests %v, want an edit", requests)
	return nil
}

func TestUpdateResponseEmbedEditsTheReplyWhoseResponseWasLost(t *testing.T) {
	stub := stubReplies(t, []int{http.StatusInternalServerError}, recentReplies)

	stub.responder.SendEmbed(&discordgo.MessageEmbed{Title: "Loading"})
	if err := discord.UpdateResponseEmbed(stub.session, stub.interaction, &discordgo.MessageEmbed{Title: "Playlist"}); err != nil {
		t.Fatalf("UpdateResponseEmbed returned %v, want nil", err)
	}

	if patch := stub.lastPatch(t); patch.Path != "/channels/c/messages/1000" {
		t.Errorf("edited %s, want the newest reply to the command, 1000", patch.Path)
	}
	if posts := stub.count(http.MethodPost, ""); posts != 1 {
		t.Errorf("sent %d messages, want only the lost loading message", posts)
	}

	reply, err := discord.GetResponseMessage(stub.session, stub.interaction)
	if err != nil || reply.ID != "1000" {
		t.Fatalf("GetResponseMessage returned %v, %v, want the recovered reply 1000", reply, err)
	}
	if lookups := stub.count(http.MethodGet, "/channels/c/messages"); lookups != 1 {
		t.Errorf("looked up the reply %d times, want once because the recovered reply is kept", lookups)
	}
}

func TestUpdateResponseEmbedPostsANewReplyWhenTheLostOneIsMissing(t *testing.T) {
	stub := stubReplies(t, []int{http.StatusInternalServerError, http.StatusOK}, `[]`)

	stub.responder.SendEmbed(&discordgo.MessageEmbed{Title: "Loading"})
	if err := discord.UpdateResponseEmbed(stub.session, stub.interaction, &discordgo.MessageEmbed{Title: "Playlist"}); err != nil {
		t.Fatalf("UpdateResponseEmbed returned %v, want nil", err)
	}

	if posts := stub.count(http.MethodPost, ""); posts != 2 {
		t.Errorf("sent %d messages, want the lost one and a fresh reply", posts)
	}
	reply, err := discord.GetResponseMessage(stub.session, stub.interaction)
	if err != nil || reply.ID != "901" {
		t.Fatalf("GetResponseMessage returned %v, %v, want the fresh reply 901", reply, err)
	}
}

func TestUpdateResponseEmbedPostsANewReplyWhenTheLookupFails(t *testing.T) {
	stub := stubReplies(t, []int{http.StatusInternalServerError, http.StatusOK}, lookupFails)

	stub.responder.SendEmbed(&discordgo.MessageEmbed{Title: "Loading"})
	if err := discord.UpdateResponseEmbed(stub.session, stub.interaction, &discordgo.MessageEmbed{Title: "Playlist"}); err != nil {
		t.Fatalf("UpdateResponseEmbed returned %v, want nil", err)
	}
	if posts := stub.count(http.MethodPost, ""); posts != 2 {
		t.Errorf("sent %d messages, want a fresh reply after the failed lookup", posts)
	}
}

func TestUpdateResponseEmbedFailsWhenNoReplyCanBeSent(t *testing.T) {
	stub := stubReplies(t, []int{http.StatusInternalServerError, http.StatusInternalServerError}, `[]`)

	stub.responder.SendEmbed(&discordgo.MessageEmbed{Title: "Loading"})
	if err := discord.UpdateResponseEmbed(stub.session, stub.interaction, &discordgo.MessageEmbed{Title: "Playlist"}); err == nil {
		t.Fatal("got nil, want an error when neither the lost reply nor a fresh one exists")
	}
}

func TestGetResponseMessageUsesTheKnownReplyWithoutAsking(t *testing.T) {
	stub := stubReplies(t, nil, recentReplies)
	stub.responder.Message = &discordgo.Message{ID: "777", ChannelID: "c"}

	reply, err := discord.GetResponseMessage(stub.session, stub.interaction)
	if err != nil || reply.ID != "777" {
		t.Fatalf("GetResponseMessage returned %v, %v, want the known reply 777", reply, err)
	}
	if requests := stub.requests(); len(requests) != 0 {
		t.Errorf("got requests %v, want none for a known reply", requests)
	}
}

func TestUpdateResponseEmbedWithComponentsRecoversTheLostReply(t *testing.T) {
	stub := stubReplies(t, []int{http.StatusInternalServerError}, recentReplies)
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Next", CustomID: "next", Style: discordgo.PrimaryButton},
	}}}

	stub.responder.SendEmbed(&discordgo.MessageEmbed{Title: "Loading"})
	if err := discord.UpdateResponseEmbedWithComponents(stub.session, stub.interaction, &discordgo.MessageEmbed{Title: "Queue"}, components); err != nil {
		t.Fatalf("UpdateResponseEmbedWithComponents returned %v, want nil", err)
	}

	patch := stub.lastPatch(t)
	if patch.Path != "/channels/c/messages/1000" {
		t.Errorf("edited %s, want the newest reply to the command, 1000", patch.Path)
	}
	if customID := discordtest.JSONAt(t, patch.Body, "components", 0, "components", 0, "custom_id"); customID != "next" {
		t.Errorf("got custom_id %v, want the button carried in the edit", customID)
	}
	if posts := stub.count(http.MethodPost, ""); posts != 1 {
		t.Errorf("sent %d messages, want only the lost loading message", posts)
	}
}

func TestUpdateResponseEmbedWithComponentsPostsANewReplyWhenTheLostOneIsMissing(t *testing.T) {
	stub := stubReplies(t, []int{http.StatusInternalServerError, http.StatusOK}, `[]`)

	stub.responder.SendEmbed(&discordgo.MessageEmbed{Title: "Loading"})
	if err := discord.UpdateResponseEmbedWithComponents(stub.session, stub.interaction, &discordgo.MessageEmbed{Title: "Queue"}, nil); err != nil {
		t.Fatalf("UpdateResponseEmbedWithComponents returned %v, want nil", err)
	}
	if posts := stub.count(http.MethodPost, ""); posts != 2 {
		t.Errorf("sent %d messages, want the lost one and a fresh reply", posts)
	}
}
