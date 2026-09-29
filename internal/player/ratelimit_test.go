package player

import (
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/messages"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

var errTooManyRequests = errors.New("failed to get stream URL: exit code 1: exit status 1\nWARNING: [youtube] f_2PqUwdijI: Unable to download webpage: HTTP Error 429: Too Many Requests")

func useQuickRetries(t *testing.T, cooldown time.Duration) {
	t.Helper()

	testutil.Swap(t, &retryDelay, time.Millisecond)
	testutil.Swap(t, &rateLimitCooldown, func() time.Duration { return cooldown })
}

func countRateLimitNotices(t *testing.T) *int {
	t.Helper()

	notices := 0
	testutil.Swap(t, &announceRateLimit, func(*discordgo.Session, string, *queue.Song) { notices++ })
	return &notices
}

func TestPlaySingleSongKeepsARateLimitedSong(t *testing.T) {
	guildID := "ratelimitedsong"
	preparedPlayer(t, guildID, 1)
	stubStreamURL(t, "", errTooManyRequests)
	useQuickRetries(t, time.Millisecond)
	notices := countRateLimitNotices(t)

	for attempt := 1; attempt <= maxRetries+2; attempt++ {
		if got := playSingleSong(nil, guildID); got != playContinue {
			t.Fatalf("attempt %d: got %v, want playContinue while YouTube is rate limiting", attempt, got)
		}
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 1 {
		t.Fatalf("got %d songs, want the rate-limited song kept", len(q.Songs))
	}
	if q.Songs[0].GetState() == queue.SongStateFailed {
		t.Error("the rate-limited song was marked failed")
	}
	if *notices != maxRetries+2 {
		t.Errorf("announced the rate limit %d times, want every rate-limited attempt reported", *notices)
	}
}

func TestPlaySingleSongPrefersARealErrorOverARateLimit(t *testing.T) {
	guildID := "ratelimitedprivate"
	preparedPlayer(t, guildID, 1)
	stubStreamURL(t, "", errors.New("ERROR: [youtube] abc: Private video. Too many requests were made to find this out"))
	useQuickRetries(t, time.Millisecond)
	notices := countRateLimitNotices(t)

	if got := playSingleSong(nil, guildID); got != playContinue {
		t.Fatalf("got %v, want playContinue after dropping the song", got)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 0 {
		t.Errorf("got %d songs, want a private video dropped at once", len(q.Songs))
	}
	if *notices != 0 {
		t.Errorf("announced a rate limit %d times for a private video", *notices)
	}
}

func TestWaitBeforeRetryStopsOnAStopSignal(t *testing.T) {
	useQuickRetries(t, time.Hour)
	player := &GuildPlayer{StopChan: make(chan struct{}, 1)}
	player.StopChan <- struct{}{}

	finished := make(chan bool, 1)
	go func() { finished <- waitBeforeRetry(player, errTooManyRequests, time.Millisecond) }()

	select {
	case isRetrying := <-finished:
		if isRetrying {
			t.Error("got true, want the stop signal to end the wait")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitBeforeRetry ignored the stop signal")
	}
}

func TestWaitBeforeRetryWaitsForTheCooldownOnlyForRateLimits(t *testing.T) {
	const cooldown = 300 * time.Millisecond
	useQuickRetries(t, cooldown)
	player := &GuildPlayer{StopChan: make(chan struct{})}

	started := time.Now()
	if !waitBeforeRetry(player, errTooManyRequests, time.Millisecond) {
		t.Fatal("got false, want the retry to go ahead")
	}
	if waited := time.Since(started); waited < cooldown {
		t.Errorf("waited %v, want at least the %v cooldown for a rate limit", waited, cooldown)
	}

	started = time.Now()
	if !waitBeforeRetry(player, errors.New("stream url unavailable"), time.Millisecond) {
		t.Fatal("got false, want the retry to go ahead")
	}
	if waited := time.Since(started); waited >= cooldown {
		t.Errorf("waited %v, want only the retry delay for an error that is not a rate limit", waited)
	}
}

func TestRateLimitNoticeIsPostedOnceAndEditedOnResume(t *testing.T) {
	guildID := "ratelimitnotice"
	setupPlayerDB(t, guildID, 1)
	t.Cleanup(func() { rateLimitNotices.remove(guildID) })
	song, q := nowPlayingFixture(guildID, false)
	sender := &fakeEmbedSender{}
	player := messages.T(guildID).Player

	postRateLimitNotice(sender, guildID, song)
	postRateLimitNotice(sender, guildID, song)

	if len(sender.sends) != 1 {
		t.Fatalf("got %d notices, want one per waiting period", len(sender.sends))
	}
	if sender.sends[0].channelID != "text" || sender.sends[0].embed.Title != player.RateLimitedTitle {
		t.Errorf("got %q in %s, want the rate limit notice in the queue's text channel", sender.sends[0].embed.Title, sender.sends[0].channelID)
	}

	deliverNowPlaying(sender, guildID, song, q)

	if len(sender.edits) != 1 || sender.edits[0].messageID != "sent" || sender.edits[0].embed.Title != player.RateLimitClearedTitle {
		t.Fatalf("got edits %+v, want the notice edited to the cleared message", sender.edits)
	}
	if rateLimitNotices.get(guildID) != nil {
		t.Error("the notice is still stored after playback resumed")
	}

	postRateLimitNotice(sender, guildID, song)
	if len(sender.sends) != 2 {
		t.Errorf("got %d notices, want a new one for the next waiting period", len(sender.sends))
	}
}
