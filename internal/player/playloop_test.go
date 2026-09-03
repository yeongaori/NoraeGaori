package player

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/queue"
	"noraegaori/internal/testutil"
)

var errNoStreamURL = errors.New("stream url unavailable")

func stubStreamURL(t *testing.T, url string, err error) {
	t.Helper()

	testutil.Swap(t, &fetchStreamURL, func(string, bool, int) (string, error) { return url, err })
}

func stubJoinVoice(t *testing.T, conn voiceConnection, err error) {
	t.Helper()

	testutil.Swap(t, &joinVoiceChannel, func(*discordgo.Session, string, string) (voiceConnection, error) {
		return conn, err
	})
}

func stubJoinVoiceResults(t *testing.T, results ...error) *int {
	t.Helper()

	calls := 0
	conn := newMockVoiceConn()

	testutil.Swap(t, &voiceRejoinDelay, time.Millisecond)
	testutil.Swap(t, &joinVoiceChannel, func(*discordgo.Session, string, string) (voiceConnection, error) {
		index := calls
		calls++
		if index < len(results) && results[index] != nil {
			return nil, results[index]
		}
		return conn, nil
	})

	return &calls
}

func stubPlayAudioResult(t *testing.T, err error) {
	t.Helper()

	testutil.Swap(t, &newAudioStream, func([]string, bool) (audioStream, error) { return boundedAudioStream(3), nil })
}

func preparedPlayer(t *testing.T, guildID string, songs int) *GuildPlayer {
	t.Helper()

	setupPlayerDB(t, guildID, songs)

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.VoiceConn = newMockVoiceConn()
	player.StopChan = make(chan struct{})
	player.PendingStream = nil
	player.mu.Unlock()

	return player
}

func TestPlaySingleSongStopsOnAnEmptyQueue(t *testing.T) {
	guildID := "singleempty"
	preparedPlayer(t, guildID, 0)

	if got := playSingleSong(nil, guildID); got != playStop {
		t.Errorf("got %v, want playStop for an empty queue", got)
	}
}

func TestPlaySingleSongStopsWhenTheVoiceJoinFails(t *testing.T) {
	guildID := "singlejoin"
	player := preparedPlayer(t, guildID, 1)

	player.mu.Lock()
	player.VoiceConn = nil
	player.mu.Unlock()

	stubJoinVoice(t, nil, errors.New("cannot join"))

	if got := playSingleSong(nil, guildID); got != playStop {
		t.Errorf("got %v, want playStop when the voice join fails", got)
	}
}

func TestPlaySingleSongRetriesTheVoiceJoinWhileTheGatewayReconnects(t *testing.T) {
	guildID := "singlegatewayback"
	player := preparedPlayer(t, guildID, 1)

	player.mu.Lock()
	player.VoiceConn = nil
	player.mu.Unlock()

	calls := stubJoinVoiceResults(t, discordgo.ErrWSNotFound, discordgo.ErrWSNotFound)

	if got := playSingleSong(nil, guildID); got == playStop {
		t.Errorf("got playStop, want playback to continue once the gateway returned")
	}
	if *calls != 3 {
		t.Errorf("got %d join attempts, want 3", *calls)
	}
}

func TestPlaySingleSongStopsWhenTheGatewayNeverReturns(t *testing.T) {
	guildID := "singlegatewaygone"
	player := preparedPlayer(t, guildID, 1)

	player.mu.Lock()
	player.VoiceConn = nil
	player.mu.Unlock()

	testutil.Swap(t, &voiceRejoinDelay, time.Millisecond)
	stubJoinVoice(t, nil, discordgo.ErrWSNotFound)

	if got := playSingleSong(nil, guildID); got != playStop {
		t.Errorf("got %v, want playStop once the rejoin budget is spent", got)
	}
}

func TestPlaySingleSongRetriesBeforeGivingUpOnAStreamURL(t *testing.T) {
	guildID := "singlenourl"
	preparedPlayer(t, guildID, 1)
	stubStreamURL(t, "", errNoStreamURL)

	if got := playSingleSong(nil, guildID); got != playContinue {
		t.Fatalf("got %v, want playContinue after a failed stream URL", got)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 1 {
		t.Errorf("got %d songs, want the song kept for a retry", len(q.Songs))
	}
}

func TestPlaySingleSongRemovesASongThatExhaustsItsRetries(t *testing.T) {
	guildID := "singleexhaust"
	preparedPlayer(t, guildID, 1)
	stubStreamURL(t, "", errNoStreamURL)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if got := playSingleSong(nil, guildID); got != playContinue {
			t.Fatalf("attempt %d: got %v, want playContinue", attempt, got)
		}
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 0 {
		t.Errorf("got %d songs left, want the song removed after %d attempts", len(q.Songs), maxRetries)
	}
}

func TestPlaySingleSongUsesTheCachedStreamURL(t *testing.T) {
	guildID := "singlecached"
	preparedPlayer(t, guildID, 1)

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	cacheNextStreamURL(t, guildID, q.Songs[0].ID, "https://example.invalid/cached")

	stubStreamURL(t, "", errors.New("the cache should have been used instead"))
	stubPlayAudioResult(t, nil)

	if got := playSingleSong(nil, guildID); got != playContinue {
		t.Errorf("got %v, want playContinue when the cached URL plays through", got)
	}
}

func TestPlaySingleSongRepeatsTheSongToTheFrontOnRepeatSingle(t *testing.T) {
	guildID := "singlerepeat"
	preparedPlayer(t, guildID, 2)

	if err := queue.SetRepeatMode(guildID, queue.RepeatSingle); err != nil {
		t.Fatalf("failed to set repeat single: %v", err)
	}

	stubStreamURL(t, "https://example.invalid/stream", nil)
	stubPlayAudioResult(t, nil)

	if got := playSingleSong(nil, guildID); got != playContinue {
		t.Fatalf("got %v, want playContinue", got)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 2 {
		t.Fatalf("got %d songs, want the song re-added once", len(q.Songs))
	}
	if q.Songs[0].Title != "Song 0" {
		t.Errorf("got %q at the front, want the repeated song back at the front", q.Songs[0].Title)
	}
	if q.Songs[1].Title != "Song 1" {
		t.Errorf("got %q second, want the untouched next song", q.Songs[1].Title)
	}
}

func TestPlaySingleSongRepeatsToTheEndOnRepeatAll(t *testing.T) {
	guildID := "singlerepeatall"
	preparedPlayer(t, guildID, 2)

	if err := queue.SetRepeatMode(guildID, queue.RepeatAll); err != nil {
		t.Fatalf("failed to set repeat all: %v", err)
	}

	stubStreamURL(t, "https://example.invalid/stream", nil)
	stubPlayAudioResult(t, nil)

	if got := playSingleSong(nil, guildID); got != playContinue {
		t.Fatalf("got %v, want playContinue", got)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 2 {
		t.Fatalf("got %d songs, want the played song moved to the end", len(q.Songs))
	}
	if q.Songs[0].Title != "Song 1" {
		t.Errorf("got %q at the front, want the second song promoted", q.Songs[0].Title)
	}
	if q.Songs[1].Title != "Song 0" {
		t.Errorf("got %q at the end, want the played song re-added there", q.Songs[1].Title)
	}
}

func TestPlaySingleSongDropsTheSongWhenRepeatIsOff(t *testing.T) {
	guildID := "singlenorepeat"
	preparedPlayer(t, guildID, 2)

	stubStreamURL(t, "https://example.invalid/stream", nil)
	stubPlayAudioResult(t, nil)

	if got := playSingleSong(nil, guildID); got != playContinue {
		t.Fatalf("got %v, want playContinue", got)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	if len(q.Songs) != 1 {
		t.Fatalf("got %d songs, want the played song dropped", len(q.Songs))
	}
	if q.Songs[0].Title != "Song 1" {
		t.Errorf("got %q, want the next song at the front", q.Songs[0].Title)
	}
}

func TestAbortPlaybackIsIdempotent(t *testing.T) {
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	abortPlayback := func() { abortOnce.Do(func() { close(abortCh) }) }

	abortPlayback()
	abortPlayback()
	abortPlayback()

	select {
	case <-abortCh:
	case <-time.After(time.Second):
		t.Fatal("the abort channel was never closed")
	}
}

func TestAnnounceGoroutineStopsWhenAborted(t *testing.T) {
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	abortPlayback := func() { abortOnce.Do(func() { close(abortCh) }) }

	firstFrameCh := make(chan struct{})
	stopChan := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		select {
		case <-firstFrameCh:
		case <-abortCh:
		case <-stopChan:
		}
	}()

	abortPlayback()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the announce goroutine outlived the abort signal")
	}
}
