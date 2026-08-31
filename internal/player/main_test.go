package player

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/queue"
	"noraegaori/internal/testutil/dbtest"
	"noraegaori/internal/testutil/localetest"
)

func TestMain(m *testing.M) {
	resumePlayback = func(*discordgo.Session, string) error { return nil }
	preCacheNext = func(string, int) {}
	announceNowPlaying = func(*discordgo.Session, string, *queue.Song, *queue.Queue) {}
	announceLeaving = func(*discordgo.Session, string, string) {}
	announceReconnect = func(*discordgo.Session, string, *queue.Song) {}
	dismissLoadingMessage = func(*discordgo.Session, string) {}
	lookupVoiceChannelBitrate = func(*discordgo.Session, string) int { return 128000 }
	announceSongError = func(*discordgo.Session, string, *queue.Song, string) {}
	announceAutoPause = func(*discordgo.Session, string, string) {}

	localetest.Run(m)
}

func setupPlayerDB(t *testing.T, guildID string, songs int) {
	t.Helper()

	dbtest.Setup(t)
	t.Cleanup(func() { DeletePlayer(guildID) })
	if err := queue.CreateQueue(guildID, "text", "voice"); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	for i := 0; i < songs; i++ {
		song := &queue.Song{
			URL:            fmt.Sprintf("https://youtube.com/watch?v=%s%d", guildID, i),
			Title:          fmt.Sprintf("Song %d", i),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		}
		if err := queue.AddSong(guildID, song, -1); err != nil {
			t.Fatalf("seed song: %v", err)
		}
	}
}
