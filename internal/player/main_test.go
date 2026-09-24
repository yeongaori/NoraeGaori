package player

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/localetest"
	"noraegaori/tests/testutil/queuetest"
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

	seeded := make([]*queue.Song, 0, songs)
	for i := 0; i < songs; i++ {
		seeded = append(seeded, &queue.Song{
			URL:            fmt.Sprintf("https://youtube.com/watch?v=%s%d", guildID, i),
			Title:          fmt.Sprintf("Song %d", i),
			Duration:       "3:00",
			RequestedByID:  "user1",
			RequestedByTag: "User#1234",
		})
	}
	queuetest.SeedWithVoice(t, guildID, "voice", seeded...)
	t.Cleanup(func() { DeletePlayer(guildID) })
}
