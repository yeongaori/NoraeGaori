package commandtest

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/discordtest"
	"noraegaori/internal/testutil/queuetest"
)

const (
	GuildID        = "command-test-guild"
	CallerID       = "caller"
	OtherID        = "other"
	VoiceChannelID = "voice-channel"
)

type Handler func(s *discordgo.Session, i *discordgo.InteractionCreate) error

type Case struct {
	Name          string
	Songs         []*queue.Song
	VoiceUsers    []string
	Options       []*discordgo.ApplicationCommandInteractionDataOption
	DiscordStatus func(*http.Request) int
	Prepare       func(t *testing.T)
	WantText      func(locale *messages.Locale) string
	Check         func(t *testing.T, reply map[string]any)
}

type Fixture struct {
	Session  *discordgo.Session
	Requests func() []discordtest.Request
	Queue    *queue.Queue
}

func NewFixture(t *testing.T, voiceUsers []string, songs ...*queue.Song) *Fixture {
	t.Helper()
	return newFixture(t, nil, voiceUsers, songs...)
}

func newFixture(t *testing.T, statusFor func(*http.Request) int, voiceUsers []string, songs ...*queue.Song) *Fixture {
	t.Helper()

	if statusFor == nil {
		statusFor = discordtest.Status(http.StatusOK)
	}
	seeded := queuetest.Seed(t, GuildID, songs...)
	session, requests := discordtest.StubAPI(t, statusFor)

	states := make([]*discordgo.VoiceState, 0, len(voiceUsers))
	for _, userID := range voiceUsers {
		states = append(states, discordtest.VoiceState(GuildID, userID, VoiceChannelID, false))
	}
	discordtest.AddGuild(t, session, GuildID, states...)

	return &Fixture{Session: session, Requests: requests, Queue: seeded}
}

func (fixture *Fixture) Component(customID string, values ...string) *discordgo.InteractionCreate {
	return discordtest.ComponentInteraction(GuildID, customID, discordtest.Member(GuildID, CallerID), values...)
}

func Run(t *testing.T, command string, handle Handler, cases []Case) {
	t.Helper()

	for index := range cases {
		testCase := &cases[index]
		t.Run(testCase.Name, func(t *testing.T) {
			fixture := newFixture(t, testCase.DiscordStatus, testCase.VoiceUsers, testCase.Songs...)
			if testCase.Prepare != nil {
				testCase.Prepare(t)
			}

			_ = handle(fixture.Session, discordtest.SlashInteraction(GuildID, command, discordtest.Member(GuildID, CallerID), testCase.Options...))

			reply := WantReplyText(t, fixture.Requests(), testCase.WantText(messages.T(GuildID)))
			if testCase.Check != nil {
				testCase.Check(t, reply)
			}
		})
	}
}

func LastReply(t *testing.T, requests []discordtest.Request) map[string]any {
	t.Helper()

	if len(requests) == 0 {
		t.Fatal("no reply was sent")
	}
	return discordtest.ReplyEmbed(t, &requests[len(requests)-1])
}

func WantReplyText(t *testing.T, requests []discordtest.Request, want string) map[string]any {
	t.Helper()

	reply := LastReply(t, requests)
	if text := discordtest.EmbedText(reply); !strings.Contains(text, want) {
		t.Errorf("the reply %q does not contain %q", text, want)
	}
	return reply
}

func ReplyContains(texts ...string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, reply map[string]any) {
		t.Helper()

		text := discordtest.EmbedText(reply)
		for _, want := range texts {
			if !strings.Contains(text, want) {
				t.Errorf("the reply %q does not contain %q", text, want)
			}
		}
	}
}

func ReplyLacks(texts ...string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, reply map[string]any) {
		t.Helper()

		text := discordtest.EmbedText(reply)
		for _, unwanted := range texts {
			if strings.Contains(text, unwanted) {
				t.Errorf("the reply %q contains %q", text, unwanted)
			}
		}
	}
}

func WantSingleResponse(t *testing.T, requests []discordtest.Request, want discordgo.InteractionResponseType) *discordtest.Request {
	t.Helper()

	if len(requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(requests))
	}
	if got := discordtest.ResponseType(&requests[0]); got != want {
		t.Errorf("response type = %d, want %d", got, want)
	}
	return &requests[0]
}

func QueueTitles(t *testing.T) []string {
	t.Helper()

	current, err := queue.GetQueue(GuildID, true)
	if err != nil || current == nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	titles := make([]string, 0, len(current.Songs))
	for _, song := range current.Songs {
		titles = append(titles, song.Title)
	}
	return titles
}

func WantTitles(want ...string) func(t *testing.T, reply map[string]any) {
	return func(t *testing.T, _ map[string]any) {
		t.Helper()

		if got := QueueTitles(t); !slices.Equal(got, want) {
			t.Errorf("queue = %v, want %v", got, want)
		}
	}
}

func Playing(t *testing.T) {
	t.Helper()
	markQueue(t, queue.SetPlaying)
}

func Loading(t *testing.T) {
	t.Helper()
	markQueue(t, queue.SetLoading)
}

func markQueue(t *testing.T, set func(guildID string, isSet bool) error) {
	t.Helper()

	if err := set(GuildID, true); err != nil {
		t.Fatalf("failed to update the queue state: %v", err)
	}
	queue.InvalidateCache(GuildID)
}

func FormatPrefix(format string) string {
	prefix, _, _ := strings.Cut(format, "%")
	return prefix
}
