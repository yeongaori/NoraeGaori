package player_test

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil/localetest"
	"noraegaori/tests/testutil/queuetest"
)

func TestMain(m *testing.M) {
	*player.HookResumePlayback = func(*discordgo.Session, string) error { return nil }
	*player.HookPreCacheNext = func(string, int) {}
	*player.HookAnnounceNowPlaying = func(*discordgo.Session, string, *queue.Song, *queue.Queue) {}
	*player.HookAnnouncePlaybackEnd = func(*discordgo.Session, string, string, bool) {}
	*player.HookAnnounceReconnect = func(*discordgo.Session, string, *queue.Song) {}
	*player.HookAnnounceRateLimit = func(*discordgo.Session, string, *queue.Song) {}
	*player.HookDismissLoadingMessage = func(*discordgo.Session, string) {}
	*player.HookLookupVoiceChannelBitrate = func(*discordgo.Session, string) int { return 128000 }
	*player.HookAnnounceSongError = func(*discordgo.Session, string, *queue.Song, string) {}
	*player.HookAnnounceAutoPause = func(*discordgo.Session, string, string) {}
	*player.HookAnnouncePlaybackCrash = func(*discordgo.Session, string, *queue.Song) {}
	*player.HookResumeAutoPaused = func(*discordgo.Session, string) {}

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
	t.Cleanup(func() { player.DeletePlayer(guildID) })
}
