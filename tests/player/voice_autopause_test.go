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
	"noraegaori/tests/testutil/discordtest"
)

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

func TestHandleVoiceStateResumesOnce(t *testing.T) {
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

			player.HandleVoiceState(session, joinEvent(guildID, "listener", "voice"))
			player.HandleVoiceState(session, joinEvent(guildID, "listener", "voice"))

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

type ignoredJoinCase struct {
	name         string
	userID       string
	channelID    string
	prepare      func(t *testing.T, guildID string, guildPlayer *player.GuildPlayer)
	wantRemember bool
}

func TestHandleVoiceStateIgnoresJoins(t *testing.T) {
	cases := ignoredJoinCases()

	for i := range cases {
		testCase := &cases[i]
		t.Run(testCase.name, func(t *testing.T) {
			guildID := fmt.Sprintf("autoresume-ignore-%d", i)
			guildPlayer := autoPausedPlayer(t, guildID, "voice")
			resumes := countAutoResumes(t)
			if testCase.prepare != nil {
				testCase.prepare(t, guildID, guildPlayer)
			}
			session := voiceSession(t, guildID, discordtest.VoiceState(guildID, testCase.userID, testCase.channelID, false))

			player.HandleVoiceState(session, joinEvent(guildID, testCase.userID, testCase.channelID))

			if got := resumes.Load(); got != 0 {
				t.Errorf("resumed %d times, want 0", got)
			}
			if got := player.HookAutoPausedChannel(guildID) == "voice"; got != testCase.wantRemember {
				t.Errorf("auto-pause remembered = %v, want %v", got, testCase.wantRemember)
			}
		})
	}
}

func ignoredJoinCases() []ignoredJoinCase {
	return []ignoredJoinCase{
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
}

func TestHandleVoiceStateStartsTimer(t *testing.T) {
	for _, isEnabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("autopause=%t", isEnabled), func(t *testing.T) {
			guildID := fmt.Sprintf("autopause-timer-%t", isEnabled)
			preparedPlayer(t, guildID, 1)
			setAutoPause(t, guildID, isEnabled)
			t.Cleanup(func() { player.HookCancelAutoPauseTimer(guildID) })
			session := voiceSession(t, guildID, discordtest.VoiceState(guildID, "bot", "voice", true))

			player.HandleVoiceState(session, joinEvent(guildID, "listener", ""))

			player.HookAutoPauseTimersMu.Lock()
			_, hasTimer := (*player.HookAutoPauseTimers)[guildID]
			player.HookAutoPauseTimersMu.Unlock()
			if hasTimer != isEnabled {
				t.Errorf("auto-pause timer started = %v, want %v", hasTimer, isEnabled)
			}
		})
	}
}

func TestHandleVoiceStateRemembers(t *testing.T) {
	guildID := "autopause-remembers-channel"
	guildPlayer, _, sessionDone := createVoicePlayer(t, guildID)
	setAutoLeave(t, guildID, false)
	testutil.Swap(t, player.HookAutoPauseDelay, time.Millisecond)
	t.Cleanup(func() {
		player.HookCancelAutoPauseTimer(guildID)
		player.HookForgetAutoPause(guildID)
	})
	endSessionOnStop(guildPlayer, sessionDone)
	session := voiceSession(t, guildID, discordtest.VoiceState(guildID, "bot", "voice", true))

	player.HandleVoiceState(session, joinEvent(guildID, "listener", ""))

	waitUntil(t, func() bool { return player.HookAutoPausedChannel(guildID) != "" }, "the auto-pause never fired")
	if got := player.HookAutoPausedChannel(guildID); got != "voice" {
		t.Errorf("remembered auto-pause channel %q, want the bot's channel voice", got)
	}
}

func TestSendAutoPauseNoticeResume(t *testing.T) {
	voiceStrings := &messages.T("g1").VoiceHandler

	for _, isResuming := range []bool{true, false} {
		t.Run(fmt.Sprintf("autoresume=%t", isResuming), func(t *testing.T) {
			guildID := fmt.Sprintf("autopause-notice-%t", isResuming)
			setupPlayerDB(t, guildID, 1)
			setAutoResume(t, guildID, isResuming)
			session, requests := discordtest.StubAPIResponder(t, respondAsDiscord)

			player.HookSendAutoPauseNotice(session, guildID, "voice")

			want := fmt.Sprintf(voiceStrings.AutoPauseDesc, "Lounge")
			if isResuming {
				want = fmt.Sprintf(voiceStrings.AutoPauseResumeDesc, "Lounge")
			}
			requested := requests()
			var sent []string
			for i := range requested {
				request := &requested[i]
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

func TestAutoPauseEmbedDescribes(t *testing.T) {
	voiceStrings := &messages.T("g1").VoiceHandler

	if got := player.HookAutoPauseEmbed("g1", "Lounge", false).Description; got != fmt.Sprintf(voiceStrings.AutoPauseDesc, "Lounge") {
		t.Errorf("manual-resume description = %q", got)
	}
	if got := player.HookAutoPauseEmbed("g1", "Lounge", true).Description; got != fmt.Sprintf(voiceStrings.AutoPauseResumeDesc, "Lounge") {
		t.Errorf("auto-resume description = %q", got)
	}
}
