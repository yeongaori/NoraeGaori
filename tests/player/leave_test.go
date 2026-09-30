package player_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"noraegaori/internal/audio/opus"
	"noraegaori/internal/audio/transition"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

type storedState struct {
	playing bool
	loading bool
	paused  bool
	songs   int
}

func readStoredState(t *testing.T, guildID string) storedState {
	t.Helper()

	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	return storedState{playing: q.Playing, loading: q.Loading, paused: q.Paused, songs: len(q.Songs)}
}

func countVoiceJoins(t *testing.T, conn player.HookVoiceConnection) *atomic.Int32 {
	t.Helper()

	var joins atomic.Int32
	testutil.Swap(t, player.HookJoinVoiceChannel, func(*discordgo.Session, string, string) (player.HookVoiceConnection, error) {
		joins.Add(1)
		return conn, nil
	})
	return &joins
}

func waitUntil(t *testing.T, condition func() bool, failure string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(failure)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLeaveDuringLoadingStopsWithoutRejoining(t *testing.T) {
	guildID := "leave-loading"
	guildPlayer := preparedPlayer(t, guildID, 1)
	resumeFrom := seedResumePoint(t, guildID, 30000)
	joins := countVoiceJoins(t, newMockVoiceConn())
	testutil.Swap(t, player.HookNewAudioStream, func(string, []string, bool, func()) (player.HookAudioStream, error) { return fakeAudioStream(), nil })
	testutil.Swap(t, player.HookFetchStreamURL, func(string, bool, int) (string, error) {
		if err := player.HookLeaveInternal(guildID); err != nil {
			t.Errorf("leave returned %v, want nil", err)
		}
		return "fake://url", nil
	})

	if got := player.HookPlaySingleSong(nil, guildID); got != player.HookPlayStop {
		t.Errorf("got %v, want playStop once the bot has left", got)
	}
	if got := joins.Load(); got != 0 {
		t.Errorf("the session joined voice %d times after leaving, want 0", got)
	}
	if guildPlayer.HookCurrentVoice() != nil {
		t.Error("the voice connection is still stored after leaving")
	}

	state := readStoredState(t, guildID)
	if state.playing || state.loading || !state.paused {
		t.Errorf("stored playing=%v loading=%v paused=%v, want false false true", state.playing, state.loading, state.paused)
	}
	if state.songs != 1 {
		t.Errorf("the queue holds %d songs, want the song kept", state.songs)
	}
	if player.IsPlaybackActive(guildID) {
		t.Error("the player still reports loading after leaving")
	}
	if got := storedSeekTime(t, guildID); got != resumeFrom {
		t.Errorf("saved position %dms, want the resume point %dms kept while nothing played", got, resumeFrom)
	}
}

func seedResumePoint(t *testing.T, guildID string, seekTime int) int {
	t.Helper()

	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		t.Fatalf("queue not ready: %v", err)
	}
	if _, err := queue.SaveSeekTime(guildID, q.Songs[0].ID, seekTime); err != nil {
		t.Fatalf("failed to seed the resume point: %v", err)
	}
	return seekTime
}

func storedSeekTime(t *testing.T, guildID string) int {
	t.Helper()

	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) == 0 {
		t.Fatalf("failed to reload the queue: %v", err)
	}
	return q.Songs[0].SeekTime
}

func TestLeaveWhilePlayingSavesThePositionAndEndsTheSession(t *testing.T) {
	guildID := "leave-playing"
	guildPlayer := preparedPlayer(t, guildID, 1)
	conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)
	joins := countVoiceJoins(t, newMockVoiceConn())
	stubStreamURL(t, "fake://url", nil)
	testutil.Swap(t, player.HookNewAudioStream, func(string, []string, bool, func()) (player.HookAudioStream, error) { return fakeAudioStream(), nil })

	if err := player.HookStartPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("session returned %v, want nil", err)
	}
	waitUntil(t, func() bool {
		guildPlayer.HookMu().Lock()
		defer guildPlayer.HookMu().Unlock()
		return guildPlayer.Playing
	}, "playback never started")
	time.Sleep(50 * time.Millisecond)

	if err := player.Leave(guildID); err != nil {
		t.Fatalf("Leave returned %v, want nil", err)
	}
	waitForPlayLockRelease(t, guildID)

	if got := conn.disconnectCount(); got != 1 {
		t.Errorf("got %d disconnects, want 1", got)
	}
	if guildPlayer.HookCurrentVoice() != nil {
		t.Error("the voice connection is still stored after leaving")
	}
	if got := joins.Load(); got != 0 {
		t.Errorf("the session joined voice %d times after leaving, want 0", got)
	}

	state := readStoredState(t, guildID)
	if state.playing || state.loading || !state.paused {
		t.Errorf("stored playing=%v loading=%v paused=%v, want false false true", state.playing, state.loading, state.paused)
	}
	if got := storedSeekTime(t, guildID); got <= 0 {
		t.Errorf("saved position %dms, want the position reached before leaving", got)
	}
}

func TestLeaveRacingAVoiceJoinDropsTheNewConnection(t *testing.T) {
	cases := []struct {
		name string
		join func(t *testing.T, guildPlayer *player.GuildPlayer, fresh *mockVoiceConn)
	}{
		{
			name: "the leave lands before the join returns",
			join: func(t *testing.T, guildPlayer *player.GuildPlayer, _ *mockVoiceConn) {
				if err := player.HookLeaveInternal(guildPlayer.GuildID); err != nil {
					t.Errorf("leave returned %v, want nil", err)
				}
			},
		},
		{
			name: "the join stored the connection before the leave halted the session",
			join: func(_ *testing.T, guildPlayer *player.GuildPlayer, fresh *mockVoiceConn) {
				guildPlayer.HookMu().Lock()
				guildPlayer.VoiceConn = fresh
				guildPlayer.VoiceChannelID = "voice"
				guildPlayer.HookHaltLocked()
				guildPlayer.HookMu().Unlock()
			},
		},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			guildID := "leave-joining-" + string(rune('a'+index))
			guildPlayer := preparedPlayer(t, guildID, 1)
			guildPlayer.HookMu().Lock()
			guildPlayer.VoiceConn = nil
			guildPlayer.HookMu().Unlock()

			fresh := newMockVoiceConn()
			testutil.Swap(t, player.HookJoinVoiceChannel, func(*discordgo.Session, string, string) (player.HookVoiceConnection, error) {
				testCase.join(t, guildPlayer, fresh)
				return fresh, nil
			})

			if got := player.HookPlaySingleSong(nil, guildID); got != player.HookPlayStop {
				t.Errorf("got %v, want playStop when the bot left during the join", got)
			}
			if guildPlayer.HookCurrentVoice() != nil {
				t.Error("the connection joined after leaving is still stored")
			}
			if got := fresh.disconnectCount(); got != 1 {
				t.Errorf("the connection joined after leaving was disconnected %d times, want 1", got)
			}
		})
	}
}

func TestLeaveDuringGatewayRetriesStopsRetrying(t *testing.T) {
	guildID := "leave-gateway"
	guildPlayer := preparedPlayer(t, guildID, 1)
	guildPlayer.HookMu().Lock()
	guildPlayer.VoiceConn = nil
	guildPlayer.HookMu().Unlock()

	var joins atomic.Int32
	testutil.Swap(t, player.HookVoiceRejoinDelay, time.Millisecond)
	testutil.Swap(t, player.HookJoinVoiceChannel, func(*discordgo.Session, string, string) (player.HookVoiceConnection, error) {
		if joins.Add(1) == 1 {
			guildPlayer.HookMu().Lock()
			guildPlayer.HookHaltLocked()
			guildPlayer.HookMu().Unlock()
			return nil, discordgo.ErrWSNotFound
		}
		return newMockVoiceConn(), nil
	})

	if got := player.HookPlaySingleSong(nil, guildID); got != player.HookPlayStop {
		t.Errorf("got %v, want playStop after leaving during the gateway outage", got)
	}
	if got := joins.Load(); got != 1 {
		t.Errorf("tried to join voice %d times, want no retry after leaving", got)
	}
}

type haltingVoiceConn struct {
	*mockVoiceConn
	player *player.GuildPlayer
	once   sync.Once
}

func (conn *haltingVoiceConn) DeadChan() <-chan struct{} {
	conn.once.Do(func() {
		conn.player.HookMu().Lock()
		conn.player.HookHaltLocked()
		conn.player.HookMu().Unlock()
	})
	return conn.mockVoiceConn.DeadChan()
}

func TestLeaveWhileCheckingTheConnectionSkipsLoading(t *testing.T) {
	guildID := "leave-checking"
	guildPlayer := preparedPlayer(t, guildID, 1)
	guildPlayer.HookMu().Lock()
	guildPlayer.VoiceConn = &haltingVoiceConn{mockVoiceConn: newMockVoiceConn(), player: guildPlayer}
	guildPlayer.HookMu().Unlock()
	stubStreamURL(t, "fake://url", nil)

	if got := player.HookPlaySingleSong(nil, guildID); got != player.HookPlayStop {
		t.Errorf("got %v, want playStop after leaving", got)
	}
	if player.IsPlaybackActive(guildID) {
		t.Error("the session marked itself loading after it was halted")
	}
	if state := readStoredState(t, guildID); state.loading {
		t.Error("the stored queue was marked loading after the session was halted")
	}
}

func TestSeekRacingALeaveDoesNotRestartPlayback(t *testing.T) {
	guildID := "leave-seeking"
	guildPlayer := preparedPlayer(t, guildID, 1)
	stubStreamURL(t, "fake://url", nil)
	var opened atomic.Int32
	testutil.Swap(t, player.HookNewAudioStream, func(string, []string, bool, func()) (player.HookAudioStream, error) {
		opened.Add(1)
		return fakeAudioStream(), nil
	})

	if err := player.HookStartPlaybackSession(nil, guildID); err != nil {
		t.Fatalf("session returned %v, want nil", err)
	}
	waitUntil(t, func() bool {
		guildPlayer.HookMu().Lock()
		defer guildPlayer.HookMu().Unlock()
		return guildPlayer.Playing
	}, "playback never started")

	guildPlayer.HookMu().Lock()
	guildPlayer.Seeking = true
	guildPlayer.SeekTargetMs = 1000
	guildPlayer.HookHaltLocked()
	guildPlayer.HookMu().Unlock()

	waitForPlayLockRelease(t, guildID)
	if got := opened.Load(); got != 1 {
		t.Errorf("opened %d streams, want the halted session not to restart for the seek", got)
	}
}

func TestLeaveWhileIdleDisconnectsWithoutPausing(t *testing.T) {
	guildID := "leave-idle"
	guildPlayer := preparedPlayer(t, guildID, 1)
	conn := guildPlayer.HookCurrentVoice().(*mockVoiceConn)

	if err := player.Leave(guildID); err != nil {
		t.Fatalf("Leave returned %v for an idle bot, want nil", err)
	}

	if got := conn.disconnectCount(); got != 1 {
		t.Errorf("got %d disconnects, want 1", got)
	}
	if guildPlayer.HookCurrentVoice() != nil {
		t.Error("the voice connection is still stored after leaving")
	}
	if state := readStoredState(t, guildID); state.paused {
		t.Error("leaving with nothing playing marked the queue paused")
	}
}

func TestPauseDuringLoadingSucceeds(t *testing.T) {
	guildID := "pause-loading"
	guildPlayer := preparedPlayer(t, guildID, 1)
	if err := queue.SetLoading(guildID, true); err != nil {
		t.Fatalf("failed to mark the queue loading: %v", err)
	}
	guildPlayer.HookMu().Lock()
	guildPlayer.Loading = true
	guildPlayer.HookMu().Unlock()

	if err := player.Pause(guildID); err != nil {
		t.Fatalf("Pause returned %v while loading, want nil", err)
	}

	state := readStoredState(t, guildID)
	if state.loading || !state.paused {
		t.Errorf("stored loading=%v paused=%v, want false true", state.loading, state.paused)
	}
}

func TestPauseWhileIdleReportsNotPlaying(t *testing.T) {
	guildID := "pause-idle"
	preparedPlayer(t, guildID, 1)

	if err := player.Pause(guildID); !errors.Is(err, player.ErrNotPlaying) {
		t.Errorf("Pause returned %v while idle, want ErrNotPlaying", err)
	}
}

func TestSeekWhileIdleReportsNotPlaying(t *testing.T) {
	guildID := "seek-idle"
	preparedPlayer(t, guildID, 1)

	if err := player.Seek(guildID, 1000); !errors.Is(err, player.ErrNotPlaying) {
		t.Errorf("Seek returned %v while idle, want ErrNotPlaying", err)
	}
}

func TestOutroFlushStopsAtADeadConnection(t *testing.T) {
	encoder, err := opus.NewEncoder(player.HookFrameRate, player.HookChannels)
	if err != nil {
		t.Fatalf("opus encoder: %v", err)
	}
	recipe := transition.DefaultRecipe()
	recipe.Effect = transition.EffectReverbCutEnd

	outro := player.HookNewOutroState()
	*outro.HookCommitted() = true
	*outro.HookProcessor() = transition.NewProcessor(recipe, 200, 0.5)
	*outro.HookTail() = (*outro.HookProcessor()).MakeTail(1)
	if *outro.HookTail() == nil {
		t.Fatal("the fixture has no tail to flush")
	}

	conn := newMockVoiceConn()
	close(conn.dead)
	sentFrames := 0
	outro.HookFlush(&player.GuildPlayer{GuildID: "outro-dead"}, conn, make(chan struct{}), encoder, &sentFrames)

	if sentFrames != 0 {
		t.Errorf("counted %d tail frames sent to a dead connection, want 0", sentFrames)
	}
	if queued := len(conn.opusSend); queued != 0 {
		t.Errorf("queued %d tail frames on a dead connection, want 0", queued)
	}
}

func TestSendFrameNeverSendsOnAClosedConnection(t *testing.T) {
	for attempt := 0; attempt < 200; attempt++ {
		conn := newMockVoiceConn()
		close(conn.dead)
		close(conn.opusSend)

		if err := player.HookSendFrame(conn, []byte{1}, make(chan struct{})); err == nil {
			t.Fatal("sendFrame reported success on a dead connection")
		}
	}
}
