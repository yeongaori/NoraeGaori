package player_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/messages"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/dbtest"
	"noraegaori/tests/testutil/discordtest"
)

func setAutoLeave(t *testing.T, guildID string, enabled bool) {
	t.Helper()
	if err := queue.SetAutoLeave(guildID, enabled); err != nil {
		t.Fatalf("failed to set auto-leave: %v", err)
	}
}

func setAutoPause(t *testing.T, guildID string, enabled bool) {
	t.Helper()
	if err := queue.SetAutoPause(guildID, enabled); err != nil {
		t.Fatalf("failed to set auto-pause: %v", err)
	}
}

func setAutoResume(t *testing.T, guildID string, enabled bool) {
	t.Helper()
	if err := queue.SetAutoResume(guildID, enabled); err != nil {
		t.Fatalf("failed to set auto-resume: %v", err)
	}
}

func isPlayerRegistered(guildID string) bool {
	player.HookPlayersMu.RLock()
	defer player.HookPlayersMu.RUnlock()
	_, exists := (*player.HookPlayers)[guildID]
	return exists
}

func hasStoredQueue(t *testing.T, guildID string) bool {
	t.Helper()
	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	return q != nil
}

func TestVoicePolicyStaysOnWhenTheSettingCannotBeRead(t *testing.T) {
	setupPlayerDB(t, "policy-seeded", 0)
	dbtest.CloseUntilCleanup(t)

	if !player.ShouldLeaveVoice("policy-uncached") || !player.HookShouldAutoPause("policy-uncached") || !player.HookShouldAutoResume("policy-uncached") {
		t.Error("an unreadable setting turned a voice policy off, want it kept on")
	}
}

func TestStartingPlaybackForgetsTheAutoPause(t *testing.T) {
	guildID := "autoresume-forget-on-start"
	autoPausedPlayer(t, guildID, "voice")
	testutil.Swap(t, player.HookPlayCurrentSong, func(*discordgo.Session, string) player.HookPlayResult { return player.HookPlayStop })

	if err := player.HookStartPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("startPlaybackSession returned %v, want nil", err)
	}

	if got := player.HookAutoPausedChannel(guildID); got != "" {
		t.Errorf("the auto-pause channel %q is still remembered after playback started", got)
	}
}

func TestStopFollowsAutoLeave(t *testing.T) {
	for _, isLeaving := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoleave=%t", isLeaving), func(t *testing.T) {
			guildID := fmt.Sprintf("stop-autoleave-%t", isLeaving)
			guildPlayer := preparedPlayer(t, guildID, 2)
			conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
			setAutoLeave(t, guildID, isLeaving)
			rememberAutoPause(t, guildID, "voice")
			cacheKey := guildID + "_1"
			player.HookPreCacheStoreMu.Lock()
			(*player.HookPreCacheStore)[cacheKey] = &player.PreCache{SongID: 1}
			player.HookPreCacheStoreMu.Unlock()

			if err := player.Stop(guildID); err != nil {
				t.Fatalf("Stop returned %v, want nil", err)
			}

			if got := player.HookAutoPausedChannel(guildID); got != "" {
				t.Errorf("stopping kept the auto-pause channel %q, want it forgotten", got)
			}
			player.HookPreCacheStoreMu.Lock()
			_, isCached := (*player.HookPreCacheStore)[cacheKey]
			player.HookPreCacheStoreMu.Unlock()
			if isCached {
				t.Error("stopping kept the pre-cached next song")
			}

			wantDisconnects := 0
			if isLeaving {
				wantDisconnects = 1
			}
			if got := conn.disconnectCount(); got != wantDisconnects {
				t.Errorf("got %d disconnects, want %d", got, wantDisconnects)
			}
			if got := isPlayerRegistered(guildID); got == isLeaving {
				t.Errorf("player registered = %v after stopping, want %v", got, !isLeaving)
			}
			if !isLeaving && guildPlayer.HookCurrentVoice() != conn {
				t.Error("stopping without leaving dropped the voice connection")
			}
			if hasStoredQueue(t, guildID) {
				t.Error("stopping kept the queue")
			}
		})
	}
}

func TestTheStopCommandFollowsAutoLeave(t *testing.T) {
	guildID := "stop-command-autoleave-off"
	guildPlayer := preparedPlayer(t, guildID, 1)
	conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
	setAutoLeave(t, guildID, false)

	if err := guildPlayer.HookDefaultDispatch(player.PlayerCommand{Type: "stop", GuildID: guildID}); err != nil {
		t.Fatalf("the stop command returned %v, want nil", err)
	}

	if got := conn.disconnectCount(); got != 0 {
		t.Errorf("got %d disconnects with auto-leave off, want 0", got)
	}
	if !isPlayerRegistered(guildID) {
		t.Error("the stop command dropped the player with auto-leave off")
	}
}

func TestLeaveDisconnectsEvenWithAutoLeaveOff(t *testing.T) {
	guildID := "leave-autoleave-off"
	guildPlayer := preparedPlayer(t, guildID, 1)
	conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
	setAutoLeave(t, guildID, false)
	guildPlayer.HookMu().Lock()
	guildPlayer.Loading = true
	guildPlayer.HookMu().Unlock()

	if err := player.Leave(guildID); err != nil {
		t.Fatalf("Leave returned %v, want nil", err)
	}

	if got := conn.disconnectCount(); got != 1 {
		t.Errorf("got %d disconnects from /leave with auto-leave off, want 1", got)
	}
	if guildPlayer.HookCurrentVoice() != nil {
		t.Error("/leave kept the voice connection with auto-leave off")
	}
}

func TestTeardownLeavesEvenWithAutoLeaveOff(t *testing.T) {
	guildID := "teardown-autoleave-off"
	guildPlayer := preparedPlayer(t, guildID, 1)
	conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
	setAutoLeave(t, guildID, false)

	if err := player.Teardown(guildID); err != nil {
		t.Fatalf("Teardown returned %v, want nil", err)
	}

	if got := conn.disconnectCount(); got != 1 {
		t.Errorf("got %d disconnects, want 1", got)
	}
	if isPlayerRegistered(guildID) {
		t.Error("teardown kept the player registered")
	}
}

func TestAnEmptyQueueEndFollowsAutoLeave(t *testing.T) {
	for _, isLeaving := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoleave=%t", isLeaving), func(t *testing.T) {
			guildID := fmt.Sprintf("queue-end-autoleave-%t", isLeaving)
			guildPlayer := preparedPlayer(t, guildID, 0)
			conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
			setAutoLeave(t, guildID, isLeaving)

			var announced []string
			testutil.Swap(t, player.HookAnnouncePlaybackEnd, func(_ *discordgo.Session, _ string, reason string, isAnnouncedLeaving bool) {
				announced = append(announced, fmt.Sprintf("%s/%t", reason, isAnnouncedLeaving))
			})

			if got := player.HookPlaySingleSong(nil, guildID); got != player.HookPlayStop {
				t.Fatalf("got %v, want playStop for an empty queue", got)
			}

			if want := fmt.Sprintf("empty/%t", isLeaving); len(announced) != 1 || announced[0] != want {
				t.Errorf("announced %v, want [%s]", announced, want)
			}
			if got := conn.disconnectCount() == 1; got != isLeaving {
				t.Errorf("disconnected = %v at the queue end, want %v", got, isLeaving)
			}
		})
	}
}

func TestPauseFollowsAutoLeave(t *testing.T) {
	for _, isLeaving := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoleave=%t", isLeaving), func(t *testing.T) {
			guildID := fmt.Sprintf("pause-autoleave-%t", isLeaving)
			guildPlayer := preparedPlayer(t, guildID, 1)
			conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
			setAutoLeave(t, guildID, isLeaving)
			guildPlayer.HookMu().Lock()
			guildPlayer.Loading = true
			guildPlayer.HookMu().Unlock()

			if err := player.Pause(guildID); err != nil {
				t.Fatalf("Pause returned %v, want nil", err)
			}

			if got := conn.disconnectCount() == 1; got != isLeaving {
				t.Errorf("disconnected = %v after pausing, want %v", got, isLeaving)
			}
			if got := guildPlayer.HookCurrentVoice() != nil; got == isLeaving {
				t.Errorf("voice connection kept = %v after pausing, want %v", got, !isLeaving)
			}
			if state := readStoredState(t, guildID); !state.paused {
				t.Error("pausing did not mark the queue paused")
			}
		})
	}
}

func endSessionOnStop(guildPlayer *player.GuildPlayer, sessionDone chan struct{}) {
	go func() {
		<-guildPlayer.StopChan
		guildPlayer.HookEndSession(sessionDone)
	}()
}

func TestPauseForEmptyChannelDoesNothingWithAutoPauseOff(t *testing.T) {
	guildID := "autopause-off"
	guildPlayer, conn, _ := playingPlayerWithVoice(t, guildID)
	setAutoPause(t, guildID, false)
	t.Cleanup(func() { player.HookForgetAutoPause(guildID) })

	player.HookPauseForEmptyChannel(discordtest.Session(t, "bot"), guildID, "voice")

	guildPlayer.HookMu().Lock()
	isPlaying, isPaused := guildPlayer.Playing, guildPlayer.Paused
	guildPlayer.HookMu().Unlock()
	if !isPlaying || isPaused {
		t.Errorf("playing=%v paused=%v with auto-pause off, want true false", isPlaying, isPaused)
	}
	if got := conn.disconnectCount(); got != 0 {
		t.Errorf("got %d disconnects with auto-pause off, want 0", got)
	}
	if got := player.HookAutoPausedChannel(guildID); got != "" {
		t.Errorf("remembered auto-pause channel %q, want none", got)
	}
}

func TestPauseForEmptyChannelStaysInVoiceWithAutoLeaveOff(t *testing.T) {
	guildID := "autopause-stay"
	guildPlayer, conn, sessionDone := playingPlayerWithVoice(t, guildID)
	setAutoLeave(t, guildID, false)
	t.Cleanup(func() { player.HookForgetAutoPause(guildID) })
	endSessionOnStop(guildPlayer, sessionDone)

	player.HookPauseForEmptyChannel(discordtest.Session(t, "bot"), guildID, "voice")

	guildPlayer.HookMu().Lock()
	isPaused := guildPlayer.Paused
	guildPlayer.HookMu().Unlock()
	if !isPaused {
		t.Error("the player is not marked as paused")
	}
	if got := conn.disconnectCount(); got != 0 {
		t.Errorf("got %d disconnects with auto-leave off, want 0", got)
	}
	if guildPlayer.HookCurrentVoice() != conn {
		t.Error("auto-pause dropped the voice connection with auto-leave off")
	}
	if got := player.HookAutoPausedChannel(guildID); got != "voice" {
		t.Errorf("remembered auto-pause channel %q, want voice", got)
	}
}

func TestTheQueueEndNoticeIsSentBeforeTheQueueIsCleared(t *testing.T) {
	guildID := "queue-end-notice"
	preparedPlayer(t, guildID, 0)
	setAutoLeave(t, guildID, false)
	session, requests := discordtest.StubAPIResponder(t, respondAsDiscord)
	testutil.Swap(t, player.HookAnnouncePlaybackEnd, player.HookSendPlaybackEndMessage)

	if got := player.HookPlaySingleSong(session, guildID); got != player.HookPlayStop {
		t.Fatalf("got %v, want playStop for an empty queue", got)
	}

	want := messages.T(guildID).Player.QueueFinishedDesc
	requested := requests()
	var sent []string
	for i := range requested {
		request := &requested[i]
		if request.Method == http.MethodPost && request.Path == "/channels/text/messages" {
			sent = append(sent, fmt.Sprint(discordtest.JSONAt(t, request.Body, "embeds", 0, "description")))
		}
	}
	if len(sent) != 1 || sent[0] != want {
		t.Errorf("sent queue-end notices %q, want [%q]", sent, want)
	}
}

func TestPrepareVoiceConnectionRejoinsOnlyForAnotherChannel(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		target    string
		wantJoins int32
	}{
		{name: "the same channel", current: "voice", target: "voice", wantJoins: 0},
		{name: "an unknown current channel", current: "", target: "voice", wantJoins: 0},
		{name: "no target channel", current: "voice", target: "", wantJoins: 0},
		{name: "another channel", current: "voice", target: "elsewhere", wantJoins: 1},
	}

	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			guildID := fmt.Sprintf("prepare-voice-%d", i)
			guildPlayer := preparedPlayer(t, guildID, 1)
			guildPlayer.HookMu().Lock()
			guildPlayer.VoiceChannelID = testCase.current
			guildPlayer.HookMu().Unlock()
			joins := countVoiceJoins(t, newMockVoiceConn())

			if err := player.HookPrepareVoiceConnection(nil, guildPlayer, guildID, testCase.target); err != nil {
				t.Fatalf("prepareVoiceConnection returned %v, want nil", err)
			}

			if got := joins.Load(); got != testCase.wantJoins {
				t.Errorf("joined %d times, want %d", got, testCase.wantJoins)
			}
			guildPlayer.HookMu().Lock()
			channelID := guildPlayer.VoiceChannelID
			guildPlayer.HookMu().Unlock()
			if testCase.wantJoins == 1 && channelID != testCase.target {
				t.Errorf("the player is in %q, want it moved to %q", channelID, testCase.target)
			}
		})
	}
}

func TestPlaybackEndEmbedOnlyMentionsLeavingWhenLeaving(t *testing.T) {
	playerStrings := &messages.T("g1").Player

	cases := []struct {
		reason      string
		isLeaving   bool
		description string
		footer      string
		color       int
	}{
		{"empty", true, playerStrings.LeavingEmptyDesc, playerStrings.LeavingEmptyFooter, messages.ColorInfo},
		{"empty", false, playerStrings.QueueFinishedDesc, playerStrings.StayingFooter, messages.ColorInfo},
		{"error", true, playerStrings.LeavingErrorDesc, playerStrings.LeavingErrorFooter, messages.ColorError},
		{"error", false, playerStrings.LeavingErrorDesc, playerStrings.StayingFooter, messages.ColorError},
	}

	for i := range cases {
		testCase := &cases[i]
		embed := player.HookPlaybackEndEmbed("g1", testCase.reason, testCase.isLeaving)
		if embed.Description != testCase.description || embed.Footer.Text != testCase.footer || embed.Color != testCase.color {
			t.Errorf("%s leaving=%v: got %q / %q / %#x, want %q / %q / %#x", testCase.reason, testCase.isLeaving,
				embed.Description, embed.Footer.Text, embed.Color, testCase.description, testCase.footer, testCase.color)
		}
	}
}
