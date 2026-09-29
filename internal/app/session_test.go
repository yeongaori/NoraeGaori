package app

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

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

func TestDiscordDebugLogsKeepMessagesWithoutTheToken(t *testing.T) {
	output := captureStandardLog(t)

	redactToken("abc.def-ghi")(discordgo.LogInformational, 0, "sending gateway websocket heartbeat seq %d", 11)

	if logged := output.String(); !strings.HasPrefix(logged, "[DG2] ") || !strings.HasSuffix(logged, "() sending gateway websocket heartbeat seq 11\n") {
		t.Errorf("got %q, want the message logged unchanged after the prefix", logged)
	}
}
