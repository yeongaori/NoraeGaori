package player

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/database"
	"noraegaori/internal/queue"
)

func TestMain(m *testing.M) {
	resumePlayback = func(*discordgo.Session, string) error { return nil }
	os.Exit(m.Run())
}

func setupPlayerDB(t *testing.T, guildID string, songs int) {
	t.Helper()
	os.RemoveAll("data")
	if err := database.Initialize(); err != nil {
		t.Fatalf("db init: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		os.RemoveAll("data")
	})
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

func TestForceSkipSpamConcurrent(t *testing.T) {
	guildID := "skipspam"
	setupPlayerDB(t, guildID, 2)

	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Skip(nil, guildID)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil && err != ErrQueueEmpty {
			t.Errorf("unexpected skip error during spam: %v", err)
		}
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if q == nil || len(q.Songs) != 0 {
		t.Errorf("expected empty queue after 5 skips on 2 songs, got %v", q)
	}
}

func TestSkipReturnsQueueEmptyOnLast(t *testing.T) {
	guildID := "skiplast"
	setupPlayerDB(t, guildID, 1)

	if err := Skip(nil, guildID); err != ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty skipping the last song, got %v", err)
	}
}

func TestDoubleSkipSafe(t *testing.T) {
	guildID := "skipdouble"
	setupPlayerDB(t, guildID, 2)

	_ = Skip(nil, guildID)
	_ = Skip(nil, guildID)

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if q == nil || len(q.Songs) != 0 {
		t.Errorf("expected empty queue after two skips, got %v", q)
	}
}

func TestSkipFadeOutGuard(t *testing.T) {
	guildID := "skipfade"
	setupPlayerDB(t, guildID, 2)

	player := GetPlayer(guildID)
	player.mu.Lock()
	player.FadingOut = true
	player.mu.Unlock()

	if err := Skip(nil, guildID); err != nil {
		t.Errorf("fade-out guard: Skip should be a no-op, got %v", err)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	if q == nil || len(q.Songs) != 2 {
		t.Errorf("fade-out guard: queue should be untouched (2), got %v", q)
	}
}

func TestPlayAudioStopsOnSignal(t *testing.T) {
	guildID := "stoptest"
	setupPlayerDB(t, guildID, 1)

	player := GetPlayer(guildID)
	mock := newMockVoiceConn()
	player.mu.Lock()
	player.VoiceConn = mock
	player.StopChan = make(chan struct{})
	player.mu.Unlock()

	orig := newAudioStream
	newAudioStream = func(args []string, collectTail bool) (*audioStream, error) {
		return fakeAudioStream(), nil
	}
	defer func() { newAudioStream = orig }()

	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		t.Fatalf("queue not ready: %v", err)
	}
	song := q.Songs[0]

	firstFrame := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- playAudio(player, song, "fake://url", 0, 100, false, 128000, firstFrame, fadeSettings{}, func(*queue.Song) {})
	}()

	select {
	case <-mock.opusSend:
	case <-time.After(3 * time.Second):
		t.Fatal("no audio frames produced by playAudio")
	}

	close(player.StopChan)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Errorf("expected playback-stopped error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("playAudio did not return after stop signal")
	}

	if !mock.speakingTrueSeen() {
		t.Error("playAudio should have signaled Speaking(true) on the voice connection")
	}
	if n := mock.disconnectCount(); n != 0 {
		t.Errorf("playAudio must not disconnect the voice connection, got %d disconnects", n)
	}
}
