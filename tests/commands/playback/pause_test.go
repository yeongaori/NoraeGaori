package playback_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"noraegaori/internal/commands/playback"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/commandtest"
	"noraegaori/tests/testutil/discordtest"
	"noraegaori/tests/testutil/queuetest"
)

func TestResumeChannel(t *testing.T) {
	fixture := commandtest.NewFixture(t, []string{commandtest.CallerID}, queuetest.SongsBy(commandtest.CallerID, 1)...)
	if err := queue.UpdateVoiceChannel(commandtest.GuildID, "old-voice"); err != nil {
		t.Fatalf("failed to store the old voice channel: %v", err)
	}
	if err := queue.SetPaused(commandtest.GuildID, true); err != nil {
		t.Fatalf("failed to pause the queue: %v", err)
	}
	testutil.Swap(t, player.HookJoinVoiceChannel, func(*discordgo.Session, string, string) (player.HookVoiceConnection, error) {
		return nil, errors.New("voice unavailable in tests")
	})
	t.Cleanup(func() {
		_ = player.Stop(commandtest.GuildID)
		player.DeletePlayer(commandtest.GuildID)
	})
	ic := discordtest.SlashInteraction(commandtest.GuildID, "resume", discordtest.Member(commandtest.GuildID, commandtest.CallerID))

	if err := playback.HandleResume(fixture.Session, ic); err != nil {
		t.Fatalf("HandleResume returned %v, want nil", err)
	}
	time.Sleep(50 * time.Millisecond)

	q, err := queue.GetQueue(commandtest.GuildID, true)
	if err != nil || q == nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	if q.VoiceChannelID != commandtest.VoiceChannelID {
		t.Errorf("stored voice channel = %q, want the caller's %q", q.VoiceChannelID, commandtest.VoiceChannelID)
	}
}

func loadingWithAutoLeave(isEnabled bool) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		if err := queue.SetAutoLeave(commandtest.GuildID, isEnabled); err != nil {
			t.Fatalf("failed to set auto-leave: %v", err)
		}
		commandtest.Loading(t)
		player.GetPlayer(commandtest.GuildID).Loading = true
		t.Cleanup(func() { player.DeletePlayer(commandtest.GuildID) })
	}
}

func TestPauseReplyFollowsAutoLeave(t *testing.T) {
	commandtest.Run(t, "pause", playback.HandlePause, []commandtest.Case{
		{
			Name:       "auto-leave on",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    loadingWithAutoLeave(true),
			WantText:   func(locale *messages.Locale) string { return locale.Descriptions.Paused },
		},
		{
			Name:       "auto-leave off",
			Songs:      queuetest.SongsBy(commandtest.CallerID, 1),
			VoiceUsers: []string{commandtest.CallerID},
			Prepare:    loadingWithAutoLeave(false),
			WantText:   func(locale *messages.Locale) string { return locale.Descriptions.PausedStay },
		},
	})
}
