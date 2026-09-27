package app

import (
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
