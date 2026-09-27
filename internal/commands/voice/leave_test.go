package voice

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

const (
	botID          = "bot"
	otherChannelID = "other-channel"
)

type playerProbe struct {
	mu      sync.Mutex
	calls   []string
	resumed chan struct{}
}

func (probe *playerProbe) record(call string) {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	probe.calls = append(probe.calls, call)
}

func (probe *playerProbe) snapshot() []string {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return append([]string(nil), probe.calls...)
}

func stubPlayer(t *testing.T, isActive bool, suspendErr, joinErr error) *playerProbe {
	t.Helper()

	probe := &playerProbe{resumed: make(chan struct{}, 1)}
	testutil.Swap(t, &isPlaybackActive, func(string) bool {
		probe.mu.Lock()
		defer probe.mu.Unlock()
		return isActive
	})
	testutil.Swap(t, &suspendPlayback, func(string) error {
		probe.record("suspend")
		probe.mu.Lock()
		isActive = false
		probe.mu.Unlock()
		return suspendErr
	})
	testutil.Swap(t, &joinVoice, func(_ *discordgo.Session, _ string, channelID string) error {
		probe.record("join " + channelID)
		return joinErr
	})
	testutil.Swap(t, &resumePlayback, func(*discordgo.Session, string) error {
		probe.record("resume")
		probe.resumed <- struct{}{}
		return nil
	})
	testutil.Swap(t, &cancelSkipVotes, func(string) { probe.record("cancel skip votes") })
	return probe
}

func voiceFixture(t *testing.T, botChannelID string, songs ...*queue.Song) *commandtest.Fixture {
	t.Helper()

	fixture := commandtest.NewFixture(t, []string{commandtest.CallerID}, songs...)
	fixture.Session.State.User = &discordgo.User{ID: botID}
	if botChannelID != "" {
		guild, err := fixture.Session.State.Guild(commandtest.GuildID)
		if err != nil {
			t.Fatalf("failed to read the seeded guild: %v", err)
		}
		guild.VoiceStates = append(guild.VoiceStates, discordtest.VoiceState(commandtest.GuildID, botID, botChannelID, true))
	}
	return fixture
}

func invoke(t *testing.T, fixture *commandtest.Fixture, name string, handle commandtest.Handler) string {
	t.Helper()

	interaction := discordtest.SlashInteraction(commandtest.GuildID, name, discordtest.Member(commandtest.GuildID, commandtest.CallerID))
	if err := handle(fixture.Session, interaction); err != nil {
		t.Errorf("the handler returned %v, want nil", err)
	}
	return discordtest.EmbedText(commandtest.LastReply(t, fixture.Requests()))
}

func wantCalls(t *testing.T, probe *playerProbe, want ...string) {
	t.Helper()

	got := probe.snapshot()
	if len(got) != len(want) {
		t.Fatalf("player calls = %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("player calls = %q, want %q", got, want)
		}
	}
}

func waitForResume(t *testing.T, probe *playerProbe) {
	t.Helper()

	select {
	case <-probe.resumed:
	case <-time.After(2 * time.Second):
		t.Fatal("playback was never resumed")
	}
}

func TestLeaveRefusesWhenTheBotIsNotInVoice(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, "")

	reply := invoke(t, fixture, "leave", HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.BotNotInVoice)
	wantCalls(t, probe)
}

func TestLeaveWithAnEmptyQueueReportsLeaving(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, commandtest.VoiceChannelID)

	reply := invoke(t, fixture, "leave", HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.LeaveSuccessDesc)
	wantCalls(t, probe, "cancel skip votes", "suspend")
}

func TestLeaveWithQueuedSongsReportsThePausedQueue(t *testing.T) {
	probe := stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, commandtest.VoiceChannelID, queuetest.SongsBy(commandtest.CallerID, 1)...)

	reply := invoke(t, fixture, "leave", HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.LeavePausedDesc)
	wantCalls(t, probe, "cancel skip votes", "suspend")
}

func TestLeaveReportsAFailedDisconnect(t *testing.T) {
	stubPlayer(t, false, errors.New("disconnect timed out"), nil)
	fixture := voiceFixture(t, commandtest.VoiceChannelID)

	reply := invoke(t, fixture, "leave", HandleLeave)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.LeaveFailedDesc)
}

func wantText(t *testing.T, reply, want string) {
	t.Helper()

	if !strings.Contains(reply, want) {
		t.Errorf("the reply %q does not contain %q", reply, want)
	}
}
