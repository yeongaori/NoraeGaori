package discordtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type Request struct {
	Method string
	Path   string
	Body   map[string]any
}

func StubAPI(t *testing.T, statusFor func(*http.Request) int) (*discordgo.Session, func() []Request) {
	t.Helper()

	var mu sync.Mutex
	var requests []Request

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		mu.Lock()
		requests = append(requests, Request{Method: r.Method, Path: r.URL.Path, Body: body})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusFor(r))
		_, _ = io.WriteString(w, `{"id":"333","channel_id":"222","user":{"id":"fetched"},"roles":["fetched-role"]}`)
	}))

	api, webhooks, channels, guilds := discordgo.EndpointAPI, discordgo.EndpointWebhooks, discordgo.EndpointChannels, discordgo.EndpointGuilds
	discordgo.EndpointAPI = server.URL + "/api/"
	discordgo.EndpointWebhooks = server.URL + "/webhooks/"
	discordgo.EndpointChannels = server.URL + "/channels/"
	discordgo.EndpointGuilds = server.URL + "/guilds/"
	t.Cleanup(func() {
		discordgo.EndpointAPI = api
		discordgo.EndpointWebhooks = webhooks
		discordgo.EndpointChannels = channels
		discordgo.EndpointGuilds = guilds
		server.Close()
	})

	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("discordgo.New returned %v, want nil", err)
	}

	return session, func() []Request {
		mu.Lock()
		defer mu.Unlock()
		return append([]Request(nil), requests...)
	}
}

func Status(status int) func(*http.Request) int {
	return func(*http.Request) int { return status }
}

func JSONAt(t *testing.T, value any, path ...any) any {
	t.Helper()

	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("expected an object before %q, got %T", key, value)
			}
			value = object[key]
		case int:
			list, ok := value.([]any)
			if !ok || key >= len(list) {
				t.Fatalf("expected a list with index %d, got %v", key, value)
			}
			value = list[key]
		}
	}
	return value
}

func Session(t *testing.T, botUserID string) *discordgo.Session {
	t.Helper()

	session := &discordgo.Session{State: discordgo.NewState()}
	session.State.User = &discordgo.User{ID: botUserID}
	return session
}

func SessionWithGuild(t *testing.T, botUserID, guildID string, states []*discordgo.VoiceState, members []*discordgo.Member) *discordgo.Session {
	t.Helper()

	session := Session(t, botUserID)
	if err := session.State.GuildAdd(&discordgo.Guild{ID: guildID, VoiceStates: states}); err != nil {
		t.Fatalf("failed to seed the guild: %v", err)
	}
	for _, member := range members {
		if err := session.State.MemberAdd(member); err != nil {
			t.Fatalf("failed to seed member %s: %v", member.User.ID, err)
		}
	}
	return session
}

func VoiceState(guildID, userID, channelID string, isBot bool) *discordgo.VoiceState {
	return &discordgo.VoiceState{
		GuildID:   guildID,
		UserID:    userID,
		ChannelID: channelID,
		Member:    &discordgo.Member{GuildID: guildID, User: &discordgo.User{ID: userID, Bot: isBot}},
	}
}

func ReactionAdd(guildID, userID, messageID, emoji string) *discordgo.MessageReactionAdd {
	return &discordgo.MessageReactionAdd{MessageReaction: messageReaction(guildID, userID, messageID, emoji)}
}

func ReactionRemove(guildID, userID, messageID, emoji string) *discordgo.MessageReactionRemove {
	return &discordgo.MessageReactionRemove{MessageReaction: messageReaction(guildID, userID, messageID, emoji)}
}

func messageReaction(guildID, userID, messageID, emoji string) *discordgo.MessageReaction {
	return &discordgo.MessageReaction{
		GuildID:   guildID,
		UserID:    userID,
		MessageID: messageID,
		Emoji:     discordgo.Emoji{Name: emoji},
	}
}
