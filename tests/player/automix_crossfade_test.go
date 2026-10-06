package player_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
)

var errFakeStream = errors.New("fake stream failure")

func seedCrossfadeQueue(t *testing.T, guildID string) *queue.Queue {
	t.Helper()

	setupPlayerDB(t, guildID, 2)

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to read the seeded queue: %v", err)
	}
	if len(q.Songs) < 2 {
		t.Fatalf("got %d songs, want 2", len(q.Songs))
	}
	return q
}

func seedCrossfadeQueueWithNext(t *testing.T, guildID string, next *queue.Song) *queue.Queue {
	t.Helper()

	setupPlayerDB(t, guildID, 1)

	if err := queue.AddSong(guildID, next, -1); err != nil {
		t.Fatalf("failed to add the next song: %v", err)
	}

	q, err := queue.GetQueue(guildID, true)
	if err != nil {
		t.Fatalf("failed to read the seeded queue: %v", err)
	}
	if len(q.Songs) < 2 {
		t.Fatalf("got %d songs, want 2", len(q.Songs))
	}
	return q
}

func cacheNextStreamURL(t *testing.T, guildID string, songID int, url string) {
	t.Helper()

	cacheKey := fmt.Sprintf("%s_%d", guildID, songID)
	player.HookPreCacheStoreMu.Lock()
	(*player.HookPreCacheStore)[cacheKey] = &player.PreCache{StreamURL: url, SongID: songID, Timestamp: time.Now()}
	player.HookPreCacheStoreMu.Unlock()

	t.Cleanup(func() {
		player.HookPreCacheStoreMu.Lock()
		delete(*player.HookPreCacheStore, cacheKey)
		player.HookPreCacheStoreMu.Unlock()
	})
}

func stubAudioStream(t *testing.T) player.HookAudioStream {
	t.Helper()

	stream := newFakeStream(0)

	testutil.Swap(t, player.HookNewAudioStream, func(string, []string, bool, func()) (player.HookAudioStream, error) { return stream, nil })

	return stream
}

func failingAudioStream(t *testing.T) {
	t.Helper()

	testutil.Swap(t, player.HookNewAudioStream, func(string, []string, bool, func()) (player.HookAudioStream, error) { return nil, errFakeStream })
}

func crossfadeFade() *player.HookFadeSettings {
	return player.HookBuildFadeSettings(player.HookFadeSettingsFields{Crossfade: true, CrossfadeSec: 6, RepeatMode: queue.RepeatOff})
}

func crossfadeEndState() *ffmpeg.EndState {
	return &ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 8000}
}

func TestCrossfadePlanArmsWithTheExpectedFrameMath(t *testing.T) {
	guildID := "planarm"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stream := stubAudioStream(t)

	guildPlayer := player.GetPlayer(guildID)
	cs := player.HookNewCrossfadeState()
	*cs.HookArmed() = false

	if planned := cs.HookPlan(guildPlayer, crossfadeEndState(), 100, 0, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade")
	}

	if !*cs.HookArmed() {
		t.Error("plan reported success but did not arm")
	}
	if *cs.HookAutoMix() {
		t.Error("a crossfade-only plan was marked as automix")
	}
	if *cs.HookCrossfadeFrames() != 300 {
		t.Errorf("got %d crossfade frames, want 300 for 6s at 50fps", *cs.HookCrossfadeFrames())
	}
	if *cs.HookTransitionFrame() != 8700 {
		t.Errorf("got transition frame %d, want 8700 (9000 total minus 300 crossfade)", *cs.HookTransitionFrame())
	}
	if *cs.HookTotalFrames() != 9000 {
		t.Errorf("got total frames %d, want the effective end 9000", *cs.HookTotalFrames())
	}
	if *cs.HookMinUsableFrames() != player.HookMinUsableCrossfadeFrames {
		t.Errorf("got %d min usable frames, want %d", *cs.HookMinUsableFrames(), player.HookMinUsableCrossfadeFrames)
	}
	if *cs.HookSlideFrames() != player.HookFallbackSlideFrames {
		t.Errorf("got %d slide frames, want the fallback %d without analysis", *cs.HookSlideFrames(), player.HookFallbackSlideFrames)
	}
	if *cs.HookNextSongID() != q.Songs[1].ID {
		t.Errorf("got next song ID %d, want %d", *cs.HookNextSongID(), q.Songs[1].ID)
	}
	if *cs.HookBStream() != stream {
		t.Error("the planned state does not hold the stream that was started")
	}
	if *cs.HookGuildID() != guildID {
		t.Errorf("got guild ID %q, want %q", *cs.HookGuildID(), guildID)
	}
	if *cs.HookBitrate() != 128000 {
		t.Errorf("got bitrate %d, want 128000", *cs.HookBitrate())
	}
	if *cs.HookProcessor() == nil {
		t.Fatal("no transition processor was built")
	}
	if aGain, bGain := (*cs.HookProcessor()).Gains(0.5); aGain == 1 && bGain == 1 {
		t.Error("crossfade gains were flat, want fade.crossfade to shape them")
	}
	if *cs.HookBeatLoop() != nil {
		t.Error("a loop-free recipe armed a beat loop")
	}
}

func TestCrossfadePlanTrimsTheSilentTailFromTheEffectiveEnd(t *testing.T) {
	guildID := "plantrim"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stubAudioStream(t)

	es := crossfadeEndState()
	es.SilentTailFrames = 500

	cs := player.HookNewCrossfadeState()
	if planned := cs.HookPlan(player.GetPlayer(guildID), es, 100, 0, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade")
	}

	if *cs.HookTotalFrames() != 8500 {
		t.Errorf("got total frames %d, want 8500 with 500 silent tail frames trimmed", *cs.HookTotalFrames())
	}
	if *cs.HookTransitionFrame() != 8200 {
		t.Errorf("got transition frame %d, want 8200", *cs.HookTransitionFrame())
	}
}

func TestCrossfadePlanClampsTheTransitionAheadOfTheCurrentFrame(t *testing.T) {
	guildID := "planclamp"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stubAudioStream(t)

	cs := player.HookNewCrossfadeState()
	if planned := cs.HookPlan(player.GetPlayer(guildID), crossfadeEndState(), 8699, 0, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade at the boundary")
	}

	if *cs.HookTransitionFrame() != 8700 {
		t.Errorf("got transition frame %d, want 8700", *cs.HookTransitionFrame())
	}
}

func TestCrossfadePlanRefusesEveryGuard(t *testing.T) {
	guildID := "planguard"
	q := seedCrossfadeQueue(t, guildID)
	nextID := q.Songs[1].ID

	cases := []struct {
		name       string
		fade       *player.HookFadeSettings
		endState   *ffmpeg.EndState
		sentFrames int
		cacheURL   string
		streamOK   bool
		mutate     func(cs *player.HookCrossfadeState)
	}{
		{
			name:     "neither automix nor crossfade",
			fade:     player.HookBuildFadeSettings(player.HookFadeSettingsFields{CrossfadeSec: 6}),
			cacheURL: "https://example.invalid/next",
			streamOK: true,
		},
		{
			name:     "already armed",
			fade:     crossfadeFade(),
			cacheURL: "https://example.invalid/next",
			streamOK: true,
			mutate:   func(cs *player.HookCrossfadeState) { *cs.HookArmed() = true },
		},
		{
			name:     "repeat single",
			fade:     player.HookBuildFadeSettings(player.HookFadeSettingsFields{Crossfade: true, CrossfadeSec: 6, RepeatMode: queue.RepeatSingle}),
			cacheURL: "https://example.invalid/next",
			streamOK: true,
		},
		{
			name:     "next song not pre-cached",
			fade:     crossfadeFade(),
			cacheURL: "",
			streamOK: true,
		},
		{
			name:       "no room left before the end",
			fade:       crossfadeFade(),
			sentFrames: 8800,
			cacheURL:   "https://example.invalid/next",
			streamOK:   true,
		},
		{
			name:     "next stream fails to start",
			fade:     crossfadeFade(),
			cacheURL: "https://example.invalid/next",
			streamOK: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.cacheURL != "" {
				cacheNextStreamURL(t, guildID, nextID, testCase.cacheURL)
			}
			if testCase.streamOK {
				stubAudioStream(t)
			} else {
				failingAudioStream(t)
			}

			es := testCase.endState
			if es == nil {
				es = crossfadeEndState()
			}

			cs := player.HookNewCrossfadeState()
			if testCase.mutate != nil {
				testCase.mutate(cs)
			}
			wasArmed := *cs.HookArmed()

			if planned := cs.HookPlan(player.GetPlayer(guildID), es, testCase.sentFrames, 0, testCase.fade, false, 128000); planned {
				t.Fatal("plan returned true, want a refusal")
			}
			if *cs.HookArmed() != wasArmed {
				t.Error("a refused plan changed the armed flag")
			}
			if *cs.HookBStream() != nil {
				t.Error("a refused plan left a stream on the state")
			}
		})
	}
}

func TestCrossfadePlanRefusesAShortNextSong(t *testing.T) {
	guildID := "planshort"
	q := seedCrossfadeQueueWithNext(t, guildID, &queue.Song{
		URL:            "https://youtube.com/watch?v=short",
		Title:          "Short",
		Duration:       "0:08",
		RequestedByID:  "user1",
		RequestedByTag: "User#1234",
	})
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stubAudioStream(t)

	cs := player.HookNewCrossfadeState()
	if planned := cs.HookPlan(player.GetPlayer(guildID), crossfadeEndState(), 100, 0, crossfadeFade(), false, 128000); planned {
		t.Error("plan returned true, want a refusal for a next song shorter than the crossfade")
	}
}

func TestCrossfadePlanRefusesALiveNextSong(t *testing.T) {
	guildID := "planlive"
	q := seedCrossfadeQueueWithNext(t, guildID, &queue.Song{
		URL:            "https://youtube.com/watch?v=live",
		Title:          "Live",
		Duration:       "0:00",
		IsLive:         true,
		RequestedByID:  "user1",
		RequestedByTag: "User#1234",
	})
	if !q.Songs[1].IsLive {
		t.Fatal("the live flag did not survive the round-trip, so this test proves nothing")
	}
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stubAudioStream(t)

	cs := player.HookNewCrossfadeState()
	if planned := cs.HookPlan(player.GetPlayer(guildID), crossfadeEndState(), 100, 0, crossfadeFade(), false, 128000); planned {
		t.Error("plan returned true, want a refusal when the next song is live")
	}
}
