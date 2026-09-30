package voice_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/voice"
	"noraegaori/internal/messages"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestSwitchVCWhilePlayingSuspendsJoinsAndResumes(t *testing.T) {
	probe := stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.CallerID, 1)...)

	reply := invoke(t, fixture, "switchvc", voice.HandleSwitchVC)
	waitForResume(t, probe)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.SwitchSuccessTitle)
	wantCalls(t, probe, "suspend", "join "+commandtest.VoiceChannelID, "resume")
}

func TestSwitchVCWhileIdleDoesNotStartPlayback(t *testing.T) {
	probe := stubPlayer(t, false, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.CallerID, 1)...)

	invoke(t, fixture, "switchvc", voice.HandleSwitchVC)
	time.Sleep(20 * time.Millisecond)

	wantCalls(t, probe, "suspend", "join "+commandtest.VoiceChannelID)
}

func TestSwitchVCReportsAFailedJoin(t *testing.T) {
	probe := stubPlayer(t, true, nil, errors.New("join timed out"))
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.CallerID, 1)...)

	reply := invoke(t, fixture, "switchvc", voice.HandleSwitchVC)
	time.Sleep(20 * time.Millisecond)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.SwitchFailedChannel)
	wantCalls(t, probe, "suspend", "join "+commandtest.VoiceChannelID)
}

func TestSwitchVCReportsAFailedQueueUpdate(t *testing.T) {
	stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.CallerID, 1)...)
	testutil.Swap(t, voice.HookJoinVoice, func(*discordgo.Session, string, string) error {
		dbtest.CloseUntilCleanup(t)
		return nil
	})

	reply := invoke(t, fixture, "switchvc", voice.HandleSwitchVC)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.SwitchFailedQueue)
}

func TestJoinMovesAPlayingBotFromAnotherChannel(t *testing.T) {
	probe := stubPlayer(t, true, nil, nil)
	fixture := voiceFixture(t, otherChannelID, queuetest.SongsBy(commandtest.CallerID, 1)...)

	reply := invoke(t, fixture, "join", voice.HandleJoin)
	waitForResume(t, probe)

	wantText(t, reply, messages.T(commandtest.GuildID).Voice.JoinSuccessTitle)
	wantCalls(t, probe, "suspend", "join "+commandtest.VoiceChannelID, "resume")
}

func TestJoinWithoutAPlayingSessionOnlyJoins(t *testing.T) {
	cases := []struct {
		name         string
		botChannelID string
		isActive     bool
	}{
		{name: "the bot is idle in another channel", botChannelID: otherChannelID},
		{name: "the bot already plays in this channel", botChannelID: commandtest.VoiceChannelID, isActive: true},
		{name: "the bot is not in voice", isActive: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			probe := stubPlayer(t, testCase.isActive, nil, nil)
			fixture := voiceFixture(t, testCase.botChannelID)

			invoke(t, fixture, "join", voice.HandleJoin)
			time.Sleep(20 * time.Millisecond)

			wantCalls(t, probe, "join "+commandtest.VoiceChannelID)
		})
	}
}
