package player_test

import (
	"context"
	"fmt"
	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/audio/ffmpeg"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/logtest"
)

func silentAudioStream(frames int) player.HookAudioStream {
	s := newFakeStream(ffmpeg.BufSize)
	s.sendFrames(frames)
	return s
}

func loudAudioStream(frames int) player.HookAudioStream {
	s := newFakeStream(ffmpeg.BufSize)
	go func() {
		defer close(s.pcm)
		for i := 0; i < frames; i++ {
			frame := make([]int16, player.HookFrameSize*player.HookChannels)
			for j := range frame {
				frame[j] = 8000
			}
			select {
			case <-s.stopChan:
				return
			case s.pcm <- frame:
			}
		}
	}()
	return s
}

func runPlayAudioWithFade(t *testing.T, guildPlayer *player.GuildPlayer, song *queue.Song, fade player.HookFadeSettings) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- player.HookPlayAudio(guildPlayer, song, "fake://url", 0, false, 128000, make(chan struct{}, 1), fade, func(*queue.Song) {})
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("playAudio did not return")
		return nil
	}
}

func TestPlayAudioSkipsLeadingSilenceWithoutSendingIt(t *testing.T) {
	guildID := "loopskip"
	guildPlayer, mock, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return silentAudioStream(20) })

	fade := *player.HookBuildFadeSettings(player.HookFadeSettingsFields{TrimSilence: true})
	if err := runPlayAudioWithFade(t, guildPlayer, song, fade); err != nil {
		t.Fatalf("playAudio returned %v, want nil: skipped silence still counts as progress", err)
	}

	if got := len(mock.opusSend); got != 0 {
		t.Errorf("got %d frames sent, want the leading silence skipped entirely", got)
	}
}

func TestPlayAudioSendsEveryFrameOfALoudStream(t *testing.T) {
	guildID := "looploud"
	guildPlayer, mock, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(12) })

	if err := runPlayAudioWithFade(t, guildPlayer, song, *player.HookBuildFadeSettings(player.HookFadeSettingsFields{TrimSilence: true})); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if got := len(mock.opusSend); got != 12 {
		t.Errorf("got %d frames sent, want all 12 loud frames", got)
	}
}

func TestPlayAudioPublishesFadeInWhileFadingAndClearsItAfter(t *testing.T) {
	guildID := "loopfadein"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(30) })

	fade := *player.HookBuildFadeSettings(player.HookFadeSettingsFields{FadeIn: true, FadeInSec: 10})

	observed := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			guildPlayer.HookMu().Lock()
			fadingIn := guildPlayer.FadingIn
			guildPlayer.HookMu().Unlock()
			if fadingIn {
				observed <- true
				return
			}
			time.Sleep(time.Millisecond)
		}
		observed <- false
	}()

	if err := runPlayAudioWithFade(t, guildPlayer, song, fade); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if !<-observed {
		t.Error("FadingIn was never published while a fade-in was configured")
	}

	guildPlayer.HookMu().Lock()
	fadingIn := guildPlayer.FadingIn
	fadingOut := guildPlayer.FadingOut
	guildPlayer.HookMu().Unlock()

	if fadingIn || fadingOut {
		t.Errorf("got FadingIn=%v FadingOut=%v after playback, want both cleared by the deferred reset", fadingIn, fadingOut)
	}
}

func TestPlayAudioLeavesTransitionArmedClearedOnReturn(t *testing.T) {
	guildID := "looparmed"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(6) })

	guildPlayer.HookTransitionArmed().Store(true)

	if err := runPlayAudioWithFade(t, guildPlayer, song, player.HookFadeSettings{}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if guildPlayer.HookTransitionArmed().Load() {
		t.Error("transitionArmed stayed set after playAudio returned")
	}
}

func TestPlayAudioConsumesTheFadeInNextFlag(t *testing.T) {
	guildID := "loopfadenext"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(6) })

	guildPlayer.HookMu().Lock()
	guildPlayer.FadeInNext = true
	guildPlayer.HookMu().Unlock()

	if err := runPlayAudioWithFade(t, guildPlayer, song, *player.HookBuildFadeSettings(player.HookFadeSettingsFields{FadeIn: true, FadeInSec: 1})); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	guildPlayer.HookMu().Lock()
	fadeInNext := guildPlayer.FadeInNext
	guildPlayer.HookMu().Unlock()

	if fadeInNext {
		t.Error("FadeInNext was not consumed by playAudio")
	}
}

func countPreCacheCalls(t *testing.T) *atomic.Int32 {
	t.Helper()

	calls := &atomic.Int32{}
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	testutil.Swap(t, player.HookPreCacheNext, func(string, int) {
		calls.Add(1)
		<-held
	})
	return calls
}

func awaitPreCacheCalls(calls *atomic.Int32, want int32) int32 {
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	return calls.Load()
}

func TestPlayAudioRechecksThePreCacheEveryInterval(t *testing.T) {
	guildID := "looprecheck"
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return loudAudioStream(3*250 + 10) })
	calls := countPreCacheCalls(t)

	if err := runPlayAudioWithFade(t, guildPlayer, song, player.HookFadeSettings{}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if got := awaitPreCacheCalls(calls, 4); got != 4 {
		t.Errorf("got %d pre-cache calls, want 4: the start plus one recheck per interval", got)
	}
}

func TestPlayAudioStopsRecheckingThePreCacheOnceTheEndIsPlanned(t *testing.T) {
	guildID := "looprecheckend"
	frames := 3*250 + 10
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream {
		s := newFakeStream(ffmpeg.BufSize)
		s.setEndState(&ffmpeg.EndState{TotalFrames: frames})
		s.sendFrames(frames)
		return s
	})
	calls := countPreCacheCalls(t)

	if err := runPlayAudioWithFade(t, guildPlayer, song, player.HookFadeSettings{}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	if got := awaitPreCacheCalls(calls, 2); got != 2 {
		t.Errorf("got %d pre-cache calls, want 2: the start and the end-of-stream plan, no rechecks after it", got)
	}
}

func addNextSong(t *testing.T, guildID string, isLive bool) *queue.Song {
	t.Helper()

	if err := queue.AddSong(guildID, &queue.Song{
		URL:            "https://youtube.com/watch?v=" + guildID + "1",
		Title:          "Song 1",
		Duration:       "3:00",
		RequestedByID:  "user1",
		RequestedByTag: "User#1234",
		IsLive:         isLive,
	}, -1); err != nil {
		t.Fatal(err)
	}
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 2 {
		t.Fatalf("queue not ready: %v", err)
	}
	return q.Songs[1]
}

func TestPlayAudioLogsTheNextSongsReadinessOnce(t *testing.T) {
	cacheHead := func(t *testing.T, guildID string, next *queue.Song) {
		cacheNextStreamURL(t, guildID, next.ID, "https://example.invalid/next")
		player.HookPreCacheStoreMu.Lock()
		(*player.HookPreCacheStore)[fmt.Sprintf("%s_%d", guildID, next.ID)].Analysis = &analysis.TrackAnalysis{BPM: 120}
		player.HookPreCacheStoreMu.Unlock()
	}
	markPending := func(t *testing.T, guildID string, next *queue.Song) {
		player.HookMarkAnalysisPending(guildID, next.ID)
		t.Cleanup(func() { player.HookClearAnalysisPending(guildID, next.ID) })
	}
	cases := []struct {
		name    string
		isLive  bool
		prepare func(t *testing.T, guildID string, next *queue.Song)
		want    string
	}{
		{"cached", false, cacheHead, "stream URL cached, head analysis cached"},
		{"cachedpending", false, func(t *testing.T, guildID string, next *queue.Song) {
			cacheHead(t, guildID, next)
			markPending(t, guildID, next)
		}, "stream URL cached, head analysis cached"},
		{"pending", false, func(t *testing.T, guildID string, next *queue.Song) {
			cacheNextStreamURL(t, guildID, next.ID, "https://example.invalid/next")
			markPending(t, guildID, next)
		}, "stream URL cached, head analysis pending"},
		{"stored", false, func(t *testing.T, _ string, next *queue.Song) {
			if err := analysis.SaveTrackAnalysis(next.URL, analysis.SegmentHead, &analysis.TrackAnalysis{BPM: 120}); err != nil {
				t.Fatal(err)
			}
		}, "stream URL missing, head analysis stored"},
		{"missing", false, nil, "stream URL missing, head analysis missing"},
		{"live", true, nil, "stream URL live, head analysis live"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			guildID := "loopreadiness" + c.name
			readLog := logtest.Capture(t)
			guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream {
				s := newFakeStream(ffmpeg.BufSize)
				s.setEndState(&ffmpeg.EndState{TotalFrames: 300})
				s.sendFrames(300)
				return s
			})
			next := addNextSong(t, guildID, c.isLive)
			if c.prepare != nil {
				c.prepare(t, guildID, next)
			}

			if err := runPlayAudioWithFade(t, guildPlayer, song, player.HookFadeSettings{}); err != nil {
				t.Fatalf("playAudio returned %v, want nil", err)
			}

			line := fmt.Sprintf("End of Song 0 known with 6.0s left; next song Song 1: %s for guild: %s", c.want, guildID)
			if got := countLogLines(readLog(), line); got != 1 {
				t.Errorf("got %d readiness lines matching %q, want 1", got, line)
			}
		})
	}
}

func TestPlayAudioReadinessCountsOnlyTheAudibleRest(t *testing.T) {
	const guildID = "loopreadinessresumed"
	readLog := logtest.Capture(t)
	resumed := newFakeStream(ffmpeg.BufSize)
	resumed.setEndState(&ffmpeg.EndState{TotalFrames: 400, SilentTailFrames: 50})
	resumed.sendFrames(300)
	guildPlayer, _, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream { return resumed })
	addNextSong(t, guildID, false)

	guildPlayer.HookMu().Lock()
	guildPlayer.PendingStream = &player.PendingStream{SongID: song.ID, Stream: resumed, FramesConsumed: 100}
	guildPlayer.HookMu().Unlock()

	if err := runPlayAudioWithFade(t, guildPlayer, song, player.HookFadeSettings{}); err != nil {
		t.Fatalf("playAudio returned %v, want nil", err)
	}

	line := fmt.Sprintf("End of Song 0 known with 5.0s left; next song Song 1: stream URL missing, head analysis missing for guild: %s", guildID)
	if got := countLogLines(readLog(), line); got != 1 {
		t.Errorf("got %d readiness lines matching %q, want 1", got, line)
	}
}

func TestSongQueuedMidSongIsReadyBeforeTheEnd(t *testing.T) {
	const guildID = "loopqueuedmidsong"
	readLog := logtest.Capture(t)
	loudFrame := func() []int16 {
		frame := make([]int16, player.HookFrameSize*player.HookChannels)
		for i := range frame {
			frame[i] = 8000
		}
		return frame
	}

	current := newFakeStream(ffmpeg.BufSize)
	var streams atomic.Int32
	guildPlayer, mock, song := newCharacterizationPlayer(t, guildID, func() player.HookAudioStream {
		if streams.Add(1) == 1 {
			return current
		}
		next := newFakeStream(ffmpeg.BufSize)
		go func() {
			for {
				select {
				case <-next.stopChan:
					return
				case next.pcm <- loudFrame():
				}
			}
		}()
		t.Cleanup(next.Stop)
		return next
	})
	enableAutoMix(t, guildID)
	t.Cleanup(func() { player.ClearPreCache(guildID) })
	t.Cleanup(func() { player.StopAnalysisBackfill(guildID) })

	testutil.Swap(t, player.HookPreCacheNext, player.PreCacheNext)
	testutil.Swap(t, player.HookAnalysisBackfillGap, 0)
	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return url, nil
	})
	testutil.Swap(t, player.HookFetchBackfillStreamURL, func(_ context.Context, url string, _ bool, _ int) (string, error) {
		return url, nil
	})
	testutil.Swap(t, player.HookAnalyzeSegment, func(_ context.Context, _ string, startSec float64, _ int) (*analysis.TrackAnalysis, error) {
		time.Sleep(50 * time.Millisecond)
		head := steadyBeat(125)
		head.Offset = startSec
		return head, nil
	})

	var sent atomic.Int64
	stopDrain := make(chan struct{})
	t.Cleanup(func() { close(stopDrain) })
	go func() {
		for {
			select {
			case <-stopDrain:
				return
			case <-mock.opusSend:
				sent.Add(1)
			}
		}
	}()

	fade := *player.HookBuildFadeSettings(player.HookFadeSettingsFields{
		AutoMix: true, Crossfade: true, TrimSilence: true, AutoMixBeats: 64, RepeatMode: queue.RepeatOff,
	})
	done := make(chan error, 1)
	go func() {
		done <- player.HookPlayAudio(guildPlayer, song, "fake://url", 0, false, 128000, make(chan struct{}, 1), fade, func(*queue.Song) {})
	}()

	pushed := 0
	push := func(until int, isDone func() bool) {
		for pushed < until && !isDone() {
			for range 10 {
				select {
				case current.pcm <- loudFrame():
					pushed++
				case <-time.After(time.Second):
					return
				}
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	never := func() bool { return false }

	push(500, never)
	next := addNextSong(t, guildID, false)
	isReady := func() bool {
		return player.GetCachedStreamURL(guildID, next.ID) != "" && player.GetCachedAnalysis(guildID, next.ID) != nil
	}
	push(7000, isReady)
	if !isReady() {
		t.Errorf("got %s not pre-cached when the end of %s was known, want URL and head analysis ready", next.Title, song.Title)
	}

	tail := steadyBeat(128)
	tail.Offset = 90
	tail.Duration = 90
	tail.FirstBeat = 0.2
	current.setEndState(&ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 4500, Analysis: tail})
	push(9000, never)
	close(current.pcm)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("playAudio did not return")
	}
	if !strings.Contains(readLog(), "bpmA=128.0 bpmB=125.0") {
		t.Error("got no AutoMix plan line, want one with both BPMs")
	}
}
