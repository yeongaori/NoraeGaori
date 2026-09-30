package player_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func respondAsDiscord(r *http.Request) (int, string) {
	if userID, isUser := strings.CutPrefix(r.URL.Path, "/users/"); isUser {
		if strings.HasPrefix(userID, "ghost") {
			return http.StatusNotFound, `{"message":"Unknown User"}`
		}
		return http.StatusOK, fmt.Sprintf(`{"id":%q,"username":%q,"bot":%t}`, userID, userID, strings.HasPrefix(userID, "bot"))
	}
	if channelID, isChannel := strings.CutPrefix(r.URL.Path, "/channels/"); isChannel && !strings.Contains(channelID, "/") {
		return http.StatusOK, fmt.Sprintf(`{"id":%q,"name":"Lounge"}`, channelID)
	}
	return http.StatusOK, `{"id":"sent"}`
}

func voiceSession(t *testing.T, guildID string, states ...*discordgo.VoiceState) *discordgo.Session {
	t.Helper()

	session, _ := discordtest.StubAPIResponder(t, respondAsDiscord)
	session.State.User = &discordgo.User{ID: "bot"}
	discordtest.AddGuild(t, session, guildID, states...)
	return session
}

func rememberAutoPause(t *testing.T, guildID, channelID string) {
	t.Helper()

	player.HookAutoPauseTimersMu.Lock()
	(*player.HookAutoPausedChannels)[guildID] = channelID
	player.HookAutoPauseTimersMu.Unlock()
	t.Cleanup(func() {
		player.HookForgetAutoPause(guildID)
		player.HookCancelAutoPauseTimer(guildID)
	})
}

func countAutoResumes(t *testing.T) *atomic.Int32 {
	t.Helper()

	var resumes atomic.Int32
	testutil.Swap(t, player.HookResumeAutoPaused, func(*discordgo.Session, string) { resumes.Add(1) })
	return &resumes
}

func autoPausedPlayer(t *testing.T, guildID, channelID string) *player.GuildPlayer {
	t.Helper()

	guildPlayer := preparedPlayer(t, guildID, 1)
	guildPlayer.HookMu().Lock()
	guildPlayer.Paused = true
	guildPlayer.HookMu().Unlock()

	rememberAutoPause(t, guildID, channelID)
	return guildPlayer
}

func joinEvent(guildID, userID, channelID string) *discordgo.VoiceStateUpdate {
	return &discordgo.VoiceStateUpdate{VoiceState: discordtest.VoiceState(guildID, userID, channelID, strings.HasPrefix(userID, "bot"))}
}

func TestAHumanJoiningTheAutoPausedChannelResumesOnce(t *testing.T) {
	for _, isBotConnected := range []bool{false, true} {
		t.Run(fmt.Sprintf("botconnected=%t", isBotConnected), func(t *testing.T) {
			guildID := fmt.Sprintf("autoresume-join-%t", isBotConnected)
			autoPausedPlayer(t, guildID, "voice")
			resumes := countAutoResumes(t)

			states := []*discordgo.VoiceState{discordtest.VoiceState(guildID, "listener", "voice", false)}
			if isBotConnected {
				states = append(states, discordtest.VoiceState(guildID, "bot", "voice", true))
			}
			session := voiceSession(t, guildID, states...)
			if err := queue.UpdateVoiceChannel(guildID, "old"); err != nil {
				t.Fatalf("failed to move the stored queue: %v", err)
			}

			player.HandleVoiceStateUpdate(session, joinEvent(guildID, "listener", "voice"))
			player.HandleVoiceStateUpdate(session, joinEvent(guildID, "listener", "voice"))

			if got := resumes.Load(); got != 1 {
				t.Errorf("resumed %d times, want 1", got)
			}
			if got := player.HookAutoPausedChannel(guildID); got != "" {
				t.Errorf("the auto-pause channel %q is still remembered after resuming", got)
			}
			q, err := queue.GetQueue(guildID, true)
			if err != nil || q == nil {
				t.Fatalf("failed to reload the queue: %v", err)
			}
			if q.VoiceChannelID != "voice" {
				t.Errorf("the queue plays in %q after resuming, want the joined channel voice", q.VoiceChannelID)
			}
		})
	}
}

func TestAutoResumeIgnoresJoinsThatShouldNotResume(t *testing.T) {
	cases := []struct {
		name         string
		userID       string
		channelID    string
		prepare      func(t *testing.T, guildID string, guildPlayer *player.GuildPlayer)
		wantRemember bool
	}{
		{name: "a bot account", userID: "bot-other", channelID: "voice", wantRemember: true},
		{name: "an unknown user", userID: "ghost", channelID: "voice", wantRemember: true},
		{
			name: "an empty queue", userID: "listener", channelID: "voice",
			prepare: func(t *testing.T, guildID string, _ *player.GuildPlayer) {
				if err := queue.RemoveFirstSong(guildID); err != nil {
					t.Fatalf("failed to empty the queue: %v", err)
				}
			},
		},
		{name: "another channel", userID: "listener", channelID: "elsewhere", wantRemember: true},
		{name: "a user leaving", userID: "listener", channelID: "", wantRemember: true},
		{
			name: "a user leaving during a manual pause", userID: "listener", channelID: "",
			prepare: func(_ *testing.T, guildID string, _ *player.GuildPlayer) { player.HookForgetAutoPause(guildID) },
		},
		{
			name: "auto-resume off", userID: "listener", channelID: "voice", wantRemember: true,
			prepare: func(t *testing.T, guildID string, _ *player.GuildPlayer) { setAutoResume(t, guildID, false) },
		},
		{
			name: "a manual resume first", userID: "listener", channelID: "voice",
			prepare: func(_ *testing.T, _ string, guildPlayer *player.GuildPlayer) {
				guildPlayer.HookMu().Lock()
				guildPlayer.Paused = false
				guildPlayer.HookMu().Unlock()
			},
		},
		{
			name: "a leave first", userID: "listener", channelID: "voice",
			prepare: func(t *testing.T, guildID string, _ *player.GuildPlayer) {
				if err := player.HookLeaveInternal(guildID); err != nil {
					t.Fatalf("leave returned %v", err)
				}
			},
		},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			guildID := fmt.Sprintf("autoresume-ignore-%d", index)
			guildPlayer := autoPausedPlayer(t, guildID, "voice")
			resumes := countAutoResumes(t)
			if testCase.prepare != nil {
				testCase.prepare(t, guildID, guildPlayer)
			}
			session := voiceSession(t, guildID, discordtest.VoiceState(guildID, testCase.userID, testCase.channelID, false))

			player.HandleVoiceStateUpdate(session, joinEvent(guildID, testCase.userID, testCase.channelID))

			if got := resumes.Load(); got != 0 {
				t.Errorf("resumed %d times, want 0", got)
			}
			if got := player.HookAutoPausedChannel(guildID) == "voice"; got != testCase.wantRemember {
				t.Errorf("auto-pause remembered = %v, want %v", got, testCase.wantRemember)
			}
		})
	}
}

func TestAnEmptyChannelStartsTheTimerOnlyWithAutoPauseOn(t *testing.T) {
	for _, isEnabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("autopause=%t", isEnabled), func(t *testing.T) {
			guildID := fmt.Sprintf("autopause-timer-%t", isEnabled)
			preparedPlayer(t, guildID, 1)
			setAutoPause(t, guildID, isEnabled)
			t.Cleanup(func() { player.HookCancelAutoPauseTimer(guildID) })
			session := voiceSession(t, guildID, discordtest.VoiceState(guildID, "bot", "voice", true))

			player.HandleVoiceStateUpdate(session, joinEvent(guildID, "listener", ""))

			player.HookAutoPauseTimersMu.Lock()
			_, hasTimer := (*player.HookAutoPauseTimers)[guildID]
			player.HookAutoPauseTimersMu.Unlock()
			if hasTimer != isEnabled {
				t.Errorf("auto-pause timer started = %v, want %v", hasTimer, isEnabled)
			}
		})
	}
}

func TestAnEmptiedChannelIsRememberedForAutoResume(t *testing.T) {
	guildID := "autopause-remembers-channel"
	guildPlayer, _, sessionDone := playingPlayerWithVoice(t, guildID)
	setAutoLeave(t, guildID, false)
	testutil.Swap(t, player.HookAutoPauseDelay, time.Millisecond)
	t.Cleanup(func() {
		player.HookCancelAutoPauseTimer(guildID)
		player.HookForgetAutoPause(guildID)
	})
	endSessionOnStop(guildPlayer, sessionDone)
	session := voiceSession(t, guildID, discordtest.VoiceState(guildID, "bot", "voice", true))

	player.HandleVoiceStateUpdate(session, joinEvent(guildID, "listener", ""))

	waitUntil(t, func() bool { return player.HookAutoPausedChannel(guildID) != "" }, "the auto-pause never fired")
	if got := player.HookAutoPausedChannel(guildID); got != "voice" {
		t.Errorf("remembered auto-pause channel %q, want the bot's channel voice", got)
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
	var sent []string
	for _, request := range requests() {
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

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			guildID := fmt.Sprintf("prepare-voice-%d", index)
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
	playerStrings := messages.T("g1").Player

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

	for _, testCase := range cases {
		embed := player.HookPlaybackEndEmbed("g1", testCase.reason, testCase.isLeaving)
		if embed.Description != testCase.description || embed.Footer.Text != testCase.footer || embed.Color != testCase.color {
			t.Errorf("%s leaving=%v: got %q / %q / %#x, want %q / %q / %#x", testCase.reason, testCase.isLeaving,
				embed.Description, embed.Footer.Text, embed.Color, testCase.description, testCase.footer, testCase.color)
		}
	}
}

func TestTheAutoPauseNoticeFollowsAutoResume(t *testing.T) {
	voiceStrings := messages.T("g1").VoiceHandler

	for _, isResuming := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoresume=%t", isResuming), func(t *testing.T) {
			guildID := fmt.Sprintf("autopause-notice-%t", isResuming)
			setupPlayerDB(t, guildID, 1)
			setAutoResume(t, guildID, isResuming)
			session, requests := discordtest.StubAPIResponder(t, respondAsDiscord)

			player.HookSendAutoPauseNotification(session, guildID, "voice")

			want := fmt.Sprintf(voiceStrings.AutoPauseDesc, "Lounge")
			if isResuming {
				want = fmt.Sprintf(voiceStrings.AutoPauseResumeDesc, "Lounge")
			}
			var sent []string
			for _, request := range requests() {
				if request.Method == http.MethodPost && request.Path == "/channels/text/messages" {
					sent = append(sent, fmt.Sprint(discordtest.JSONAt(t, request.Body, "embeds", 0, "description")))
				}
			}
			if len(sent) != 1 || sent[0] != want {
				t.Errorf("sent notices %q, want [%q]", sent, want)
			}
		})
	}
}

func TestAutoPauseEmbedDescribesHowPlaybackComesBack(t *testing.T) {
	voiceStrings := messages.T("g1").VoiceHandler

	if got := player.HookAutoPauseEmbed("g1", "Lounge", false).Description; got != fmt.Sprintf(voiceStrings.AutoPauseDesc, "Lounge") {
		t.Errorf("manual-resume description = %q", got)
	}
	if got := player.HookAutoPauseEmbed("g1", "Lounge", true).Description; got != fmt.Sprintf(voiceStrings.AutoPauseResumeDesc, "Lounge") {
		t.Errorf("auto-resume description = %q", got)
	}
}
