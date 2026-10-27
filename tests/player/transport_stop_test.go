package player_test

import (
	"sync/atomic"
	"testing"
	"time"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
)

type runningSession struct {
	stop      chan struct{}
	isEnded   atomic.Bool
	pending   *fakeStream
	guildID   string
	guildPlay *player.GuildPlayer
}

func startRunningSession(t *testing.T, guildID string, isPlaying, isLoading bool) *runningSession {
	t.Helper()

	session := &runningSession{
		stop:      make(chan struct{}),
		pending:   newFakeStream(0),
		guildID:   guildID,
		guildPlay: player.GetPlayer(guildID),
	}
	done := make(chan struct{}, 1)
	done <- struct{}{}

	session.guildPlay.HookMu().Lock()
	session.guildPlay.Playing = isPlaying
	session.guildPlay.Loading = isLoading
	session.guildPlay.StopChan = session.stop
	session.guildPlay.PlaybackDone = done
	session.guildPlay.PendingStream = &player.PendingStream{Stream: session.pending}
	session.guildPlay.HookMu().Unlock()

	go func() {
		<-session.stop
		time.Sleep(100 * time.Millisecond)
		session.isEnded.Store(true)
		done <- struct{}{}
	}()
	return session
}

func (session *runningSession) isStopSent() bool {
	select {
	case <-session.stop:
		return true
	default:
		return false
	}
}

func (session *runningSession) isPendingStopped() bool {
	select {
	case <-session.pending.stopChan:
		return true
	default:
		return false
	}
}

func (session *runningSession) wantState(t *testing.T, isPendingKept bool) {
	t.Helper()

	session.guildPlay.HookMu().Lock()
	isPlaying := session.guildPlay.Playing
	pending := session.guildPlay.PendingStream
	session.guildPlay.HookMu().Unlock()

	if isPlaying {
		t.Errorf("playing: got %v, want false", isPlaying)
	}
	if isPendingKept && pending == nil {
		t.Error("pending stream: got nil, want kept")
	}
	if !isPendingKept && pending != nil {
		t.Errorf("pending stream: got %v, want nil", pending)
	}
	if isStopped := session.isPendingStopped(); isStopped == isPendingKept {
		t.Errorf("pending stream stopped: got %v, want %v", isStopped, !isPendingKept)
	}
}

func runWithin(t *testing.T, name string, call func() error) error {
	t.Helper()

	returned := make(chan error, 1)
	go func() { returned <- call() }()
	select {
	case err := <-returned:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never returned", name)
		return nil
	}
}

func wantQueueLength(t *testing.T, guildID string, want int) {
	t.Helper()

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	if got := len(q.Songs); got != want {
		t.Errorf("queue length: got %d, want %d", got, want)
	}
}

func checkSkipEndsPlayingSession(t *testing.T, guildID, name string, skip func() error, wantPending bool, wantLength int) {
	t.Helper()

	setupPlayerDB(t, guildID, 2)
	session := startRunningSession(t, guildID, true, false)

	if err := runWithin(t, name, skip); err != nil {
		t.Fatalf("%s returned %v", name, err)
	}

	if got := session.isStopSent(); !got {
		t.Errorf("stop signal: got sent=%v, want sent=%v", got, true)
	}
	if !session.isEnded.Load() {
		t.Errorf("%s returned before the playing session ended", name)
	}
	session.wantState(t, wantPending)
	wantQueueLength(t, guildID, wantLength)
}

func TestSkipWhilePlayingStopsTheSessionAndDropsThePendingStream(t *testing.T) {
	const guildID = "skip-playing"
	checkSkipEndsPlayingSession(t, guildID, "Skip", func() error { return player.Skip(nil, guildID) }, false, 1)
}

func TestSkipToWhilePlayingKeepsThePendingStream(t *testing.T) {
	const guildID = "skipto-playing"
	checkSkipEndsPlayingSession(t, guildID, "SkipTo", func() error { return player.SkipTo(nil, guildID) }, true, 2)
}

func TestInternalSkipIgnoresALoadingSession(t *testing.T) {
	const guildID = "skip-loading"
	setupPlayerDB(t, guildID, 3)

	internal := startRunningSession(t, guildID, false, true)
	if err := runWithin(t, "skipInternal", func() error { return player.HookSkipInternal(nil, guildID) }); err != nil {
		t.Fatalf("skipInternal returned %v", err)
	}
	if got := internal.isStopSent(); got {
		t.Errorf("stop signal: got sent=%v, want sent=%v", got, false)
	}
	if internal.isPendingStopped() {
		t.Errorf("pending stream stopped: got %v, want %v", true, false)
	}
	wantQueueLength(t, guildID, 2)

	command := startRunningSession(t, guildID, false, true)
	if err := runWithin(t, "Skip", func() error { return player.Skip(nil, guildID) }); err != nil {
		t.Fatalf("Skip returned %v", err)
	}
	if got := command.isStopSent(); !got {
		t.Errorf("stop signal: got sent=%v, want sent=%v", got, true)
	}
	wantQueueLength(t, guildID, 1)
}

func TestPlaybackEndedCallback(t *testing.T) {
	const guildID = "ended-callback"
	preparedPlayer(t, guildID, 1)

	var ended, started atomic.Int32
	player.SetOnPlaybackEndedCallback(func(string) { ended.Add(1) })
	player.SetOnSongStartCallback(func(string) { started.Add(1) })
	t.Cleanup(func() {
		player.SetOnPlaybackEndedCallback(nil)
		player.SetOnSongStartCallback(nil)
	})

	if err := runWithin(t, "Stop", func() error { return player.Stop(guildID) }); err != nil {
		t.Fatalf("Stop returned %v", err)
	}
	if got := ended.Load(); got != 1 {
		t.Errorf("ended callback ran %d times, want 1", got)
	}
	if got := started.Load(); got != 0 {
		t.Errorf("song start callback ran %d times, want 0", got)
	}
}

func TestStopWhilePlayingWaitsForTheSession(t *testing.T) {
	const guildID = "stop-playing"
	setupPlayerDB(t, guildID, 2)
	session := startRunningSession(t, guildID, true, false)

	if err := runWithin(t, "Stop", func() error { return player.Stop(guildID) }); err != nil {
		t.Fatalf("Stop returned %v", err)
	}

	if got := session.isStopSent(); !got {
		t.Errorf("stop signal: got sent=%v, want sent=%v", got, true)
	}
	if !session.isEnded.Load() {
		t.Error("Stop returned before the playing session ended")
	}
}
