package app_test

import (
	"bytes"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/logtest"

	"noraegaori/internal/app"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/dbtest"
)

func TestGuildRemovalDropsThePlayerEvenWithAutoLeaveOff(t *testing.T) {
	const guildID = "removed-guild"
	dbtest.Setup(t)
	if err := queue.SetAutoLeave(guildID, false); err != nil {
		t.Fatalf("failed to turn auto-leave off: %v", err)
	}
	before := player.GetPlayer(guildID)
	t.Cleanup(func() { player.DeletePlayer(guildID) })

	app.HookOnGuildDelete(nil, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: guildID}})

	if player.GetPlayer(guildID) == before {
		t.Error("the removed guild's player is still registered, want it torn down")
	}
}

func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var output bytes.Buffer
	previousWriter, previousFlags := log.Writer(), log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})
	return &output
}

func TestDiscordDebugLogsHideTheToken(t *testing.T) {
	output := captureStandardLog(t)
	identify := struct{ Token string }{"Bot abc.def-ghi"}

	app.HookRedactToken("abc.def-ghi")(discordgo.LogDebug, 0, "Identify Packet: \n%#v, resumed with %s", identify, "abc.def-ghi")

	logged := output.String()
	if strings.Contains(logged, "abc.def-ghi") {
		t.Errorf("got %q, want the token hidden", logged)
	}
	if count := strings.Count(logged, "[token]"); count != 2 {
		t.Errorf("got %q, want both copies of the token replaced", logged)
	}
	if !strings.HasPrefix(logged, "[DG3] session_test.go:") || !strings.Contains(logged, ":TestDiscordDebugLogsHideTheToken() ") {
		t.Errorf("got %q, want the level and the calling test in the prefix", logged)
	}
}

func useConnectionState(t *testing.T, isStopping bool) *atomic.Int32 {
	t.Helper()

	var resumes atomic.Int32
	testutil.Swap(t, app.HookIsShuttingDown, func() bool { return isStopping })
	testutil.Swap(t, app.HookResumeAfterReconnect, func(*discordgo.Session) { resumes.Add(1) })
	app.HookIsDisconnected.Store(false)
	t.Cleanup(func() { app.HookIsDisconnected.Store(false) })
	return &resumes
}

func requireResumes(t *testing.T, resumes *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for resumes.Load() < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := resumes.Load(); got != want {
		t.Errorf("resumed playback %d times, want %d", got, want)
	}
}

const (
	lostConnection = "Lost the connection to Discord, reconnecting"
	reconnected    = "Reconnected to Discord"
)

func TestLosingDiscordLogsOnceUntilReconnected(t *testing.T) {
	read := logtest.CaptureConsole(t)
	resumes := useConnectionState(t, false)

	app.HookOnConnect(nil, &discordgo.Connect{})
	app.HookOnDisconnect(nil, &discordgo.Disconnect{})
	app.HookOnDisconnect(nil, &discordgo.Disconnect{})
	app.HookOnConnect(nil, &discordgo.Connect{})
	app.HookOnConnect(nil, &discordgo.Connect{})
	app.HookOnDisconnect(nil, &discordgo.Disconnect{})

	requireResumes(t, resumes, 1)
	logged := read()
	if count := strings.Count(logged, lostConnection); count != 2 {
		t.Errorf("logged %d losses, want once per outage (2) in %q", count, logged)
	}
	if count := strings.Count(logged, reconnected); count != 1 {
		t.Errorf("reported %d reconnects, want only the one after an outage in %q", count, logged)
	}
}

func TestClosingTheSessionOnShutdownIsNotLogged(t *testing.T) {
	read := logtest.CaptureConsole(t)
	resumes := useConnectionState(t, true)

	app.HookOnDisconnect(nil, &discordgo.Disconnect{})
	app.HookOnConnect(nil, &discordgo.Connect{})

	if logged := read(); strings.Contains(logged, lostConnection) {
		t.Errorf("got %q, want the shutdown disconnect kept quiet", logged)
	}
	requireResumes(t, resumes, 0)
}

func TestResumingAfterAReconnectWaitsAndSkipsShutdown(t *testing.T) {
	for _, isStopping := range []bool{false, true} {
		calls := 0
		testutil.Swap(t, app.HookIsShuttingDown, func() bool { return isStopping })
		testutil.Swap(t, app.HookResumeWaitingPlayers, func(*discordgo.Session) { calls++ })
		testutil.Swap(t, app.HookReconnectResumeDelay, 30*time.Millisecond)

		started := time.Now()
		app.HookResumePlayersAfterReconnect(nil)

		if waited := time.Since(started); waited < 30*time.Millisecond {
			t.Errorf("shutting down=%v: resumed after %v, want the reconnect delay first", isStopping, waited)
		}
		if want := map[bool]int{false: 1, true: 0}[isStopping]; calls != want {
			t.Errorf("shutting down=%v: resumed players %d times, want %d", isStopping, calls, want)
		}
	}
}

func TestFirstConnectDoesNotResumePlayback(t *testing.T) {
	resumes := useConnectionState(t, false)

	app.HookOnConnect(nil, &discordgo.Connect{})

	requireResumes(t, resumes, 0)
}

func TestDiscordDebugLogsKeepMessagesWithoutTheToken(t *testing.T) {
	output := captureStandardLog(t)

	app.HookRedactToken("abc.def-ghi")(discordgo.LogInformational, 0, "sending gateway websocket heartbeat seq %d", 11)

	if logged := output.String(); !strings.HasPrefix(logged, "[DG2] ") || !strings.HasSuffix(logged, "() sending gateway websocket heartbeat seq 11\n") {
		t.Errorf("got %q, want the message logged unchanged after the prefix", logged)
	}
}
