package play

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func recordSearchEditRequests(t *testing.T) *[]string {
	t.Helper()

	var mu sync.Mutex
	paths := make([]string, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"333","channel_id":"222"}`)
	}))

	channels, webhooks := discordgo.EndpointChannels, discordgo.EndpointWebhooks
	discordgo.EndpointChannels = server.URL + "/channels/"
	discordgo.EndpointWebhooks = server.URL + "/webhooks/"

	expiry := searchSelectionExpiry
	searchSelectionExpiry = 10 * time.Millisecond

	t.Cleanup(func() {
		searchSelectionExpiry = expiry
		discordgo.EndpointChannels = channels
		discordgo.EndpointWebhooks = webhooks
		server.Close()
	})

	return &paths
}

func newPseudoSearchSelection(t *testing.T) *searchSelection {
	t.Helper()

	return &searchSelection{
		searchMessageID: "g1_u1_1",
		original: &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				Type:      discordgo.InteractionApplicationCommand,
				GuildID:   "g1",
				ChannelID: "222",
				Token:     "message_111_222",
			},
		},
		panelMsg: &discordgo.Message{ID: "333", ChannelID: "222"},
		done:     make(chan struct{}),
	}
}

func TestExpireSearchSelectionEditsTheChannelMessage(t *testing.T) {
	paths := recordSearchEditRequests(t)

	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("discordgo.New returned %v, want nil", err)
	}

	expireSearchSelection(session, newPseudoSearchSelection(t), func() {})

	if len(*paths) != 1 {
		t.Fatalf("requested %v, want exactly one edit", *paths)
	}
	if (*paths)[0] != "PATCH /channels/222/messages/333" {
		t.Fatalf("requested %q, want PATCH /channels/222/messages/333", (*paths)[0])
	}
	for _, path := range *paths {
		if strings.Contains(path, "/webhooks/") {
			t.Errorf("requested %q, want no interaction webhook call for a text command", path)
		}
	}
}

func TestExpireSearchSelectionSkipsTheEditWithoutAPanelMessage(t *testing.T) {
	paths := recordSearchEditRequests(t)

	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("discordgo.New returned %v, want nil", err)
	}

	selection := newPseudoSearchSelection(t)
	selection.panelMsg = nil

	expireSearchSelection(session, selection, func() {})

	if len(*paths) != 0 {
		t.Errorf("requested %v, want no request when the panel message cannot be resolved", *paths)
	}
}

func TestExpireSearchSelectionSkipsTheEditWhenDone(t *testing.T) {
	paths := recordSearchEditRequests(t)

	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("discordgo.New returned %v, want nil", err)
	}

	selection := newPseudoSearchSelection(t)
	close(selection.done)

	cleaned := false
	expireSearchSelection(session, selection, func() { cleaned = true })

	if len(*paths) != 0 {
		t.Errorf("requested %v, want no requests once the selection completed", *paths)
	}
	if !cleaned {
		t.Error("cleanup did not run when the selection completed")
	}
}
