package app

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/logger"
	"noraegaori/tests/testutil"

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

	onGuildDelete(nil, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: guildID}})

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

	redactToken("abc.def-ghi")(discordgo.LogDebug, 0, "Identify Packet: \n%#v, resumed with %s", identify, "abc.def-ghi")

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

func captureBotLog(t *testing.T) func() string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bot.log")
	logger.SetLogFile(path)
	t.Cleanup(func() { logger.SetLogFile("") })

	return func() string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read the captured log: %v", err)
		}
		return string(content)
	}
}

func useConnectionState(t *testing.T, isStopping bool) *atomic.Int32 {
	t.Helper()

	var resumes atomic.Int32
	testutil.Swap(t, &isShuttingDown, func() bool { return isStopping })
	testutil.Swap(t, &resumeAfterReconnect, func(*discordgo.Session) { resumes.Add(1) })
	isDisconnected.Store(false)
	t.Cleanup(func() { isDisconnected.Store(false) })
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

func TestLosingDiscordWarnsOnceUntilReconnected(t *testing.T) {
	read := captureBotLog(t)
	resumes := useConnectionState(t, false)

	onConnect(nil, &discordgo.Connect{})
	onDisconnect(nil, &discordgo.Disconnect{})
	onDisconnect(nil, &discordgo.Disconnect{})
	onConnect(nil, &discordgo.Connect{})
	onConnect(nil, &discordgo.Connect{})
	onDisconnect(nil, &discordgo.Disconnect{})

	requireResumes(t, resumes, 1)
	logged := read()
	if count := strings.Count(logged, lostConnection); count != 2 {
		t.Errorf("warned %d times, want once per outage (2) in %q", count, logged)
	}
	if count := strings.Count(logged, reconnected); count != 1 {
		t.Errorf("reported %d reconnects, want only the one after an outage in %q", count, logged)
	}
	if !strings.Contains(logged, "WARN") {
		t.Errorf("got %q, want the lost connection logged as a warning", logged)
	}
}

func TestClosingTheSessionOnShutdownIsNotAWarning(t *testing.T) {
	read := captureBotLog(t)
	resumes := useConnectionState(t, true)

	onDisconnect(nil, &discordgo.Disconnect{})
	onConnect(nil, &discordgo.Connect{})

	if logged := read(); strings.Contains(logged, lostConnection) {
		t.Errorf("got %q, want the shutdown disconnect kept quiet", logged)
	}
	requireResumes(t, resumes, 0)
}

func TestResumingAfterAReconnectWaitsAndSkipsShutdown(t *testing.T) {
	for _, isStopping := range []bool{false, true} {
		calls := 0
		testutil.Swap(t, &isShuttingDown, func() bool { return isStopping })
		testutil.Swap(t, &resumeWaitingPlayers, func(*discordgo.Session) { calls++ })
		testutil.Swap(t, &reconnectResumeDelay, 30*time.Millisecond)

		started := time.Now()
		resumePlayersAfterReconnect(nil)

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

	onConnect(nil, &discordgo.Connect{})

	requireResumes(t, resumes, 0)
}

func TestDiscordDebugLogsKeepMessagesWithoutTheToken(t *testing.T) {
	output := captureStandardLog(t)

	redactToken("abc.def-ghi")(discordgo.LogInformational, 0, "sending gateway websocket heartbeat seq %d", 11)

	if logged := output.String(); !strings.HasPrefix(logged, "[DG2] ") || !strings.HasSuffix(logged, "() sending gateway websocket heartbeat seq 11\n") {
		t.Errorf("got %q, want the message logged unchanged after the prefix", logged)
	}
}
