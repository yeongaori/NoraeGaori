package player

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestSetLoadingMessageClearsAnnouncement(t *testing.T) {
	guildID := "loadingannounce"
	t.Cleanup(func() {
		DeleteLoadingMessage(guildID)
		clearAnnounced(guildID)
	})

	markAnnounced(guildID, 7)

	SetLoadingMessage(guildID, &discordgo.Message{ID: "msg", ChannelID: "channel"})

	if GetLoadingMessage(guildID) == nil {
		t.Fatal("loading message was not stored")
	}

	if !markAnnounced(guildID, 7) {
		t.Error("stale announcement would leave the loading message unresolved")
	}
}
