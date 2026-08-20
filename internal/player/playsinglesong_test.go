package player

import (
	"errors"
	"testing"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/queue"
)

var errNoStreamURL = errors.New("stream url unavailable")

func stubStreamURL(t *testing.T, url string, err error) {
	t.Helper()

	previous := fetchStreamURL
	fetchStreamURL = func(string, bool, int) (string, error) { return url, err }
	t.Cleanup(func() { fetchStreamURL = previous })
}

func stubJoinVoice(t *testing.T, conn voiceConnection, err error) {
	t.Helper()

	previous := joinVoiceChannel
	joinVoiceChannel = func(*discordgo.Session, string, string) (voiceConnection, error) {
		return conn, err
	}
	t.Cleanup(func() { joinVoiceChannel = previous })
}

func stubPlayAudioResult(t *testing.T, err error) {
	t.Helper()

	previous := newAudioStream
	newAudioStream = func([]string, bool) (audioStream, error) { return boundedAudioStream(3), nil }
	t.Cleanup(func() { newAudioStream = previous })
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
