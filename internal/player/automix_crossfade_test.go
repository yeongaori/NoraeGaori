package player

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"noraegaori/internal/audio/ffmpeg"
	"noraegaori/internal/queue"
	"noraegaori/internal/testutil"
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
	preCacheStoreMu.Lock()
	preCacheStore[cacheKey] = &PreCache{StreamURL: url, SongID: songID, Timestamp: time.Now()}
	preCacheStoreMu.Unlock()

	t.Cleanup(func() {
		preCacheStoreMu.Lock()
		delete(preCacheStore, cacheKey)
		preCacheStoreMu.Unlock()
	})
}

func stubAudioStream(t *testing.T) audioStream {
	t.Helper()

	stream := newFakeStream(0)

	testutil.Swap(t, &newAudioStream, func([]string, bool) (audioStream, error) { return stream, nil })

	return stream
}

func failingAudioStream(t *testing.T) {
	t.Helper()

	testutil.Swap(t, &newAudioStream, func([]string, bool) (audioStream, error) { return nil, errFakeStream })
}

func crossfadeFade() fadeSettings {
	return fadeSettings{crossfade: true, crossfadeSec: 6, repeatMode: queue.RepeatOff}
}

func crossfadeEndState() *ffmpeg.EndState {
	return &ffmpeg.EndState{TotalFrames: 9000, TailStartFrame: 8000}
}

func TestCrossfadePlanArmsWithTheExpectedFrameMath(t *testing.T) {
	guildID := "planarm"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stream := stubAudioStream(t)

	player := GetPlayer(guildID)
	cs := newCrossfadeState()
	cs.armed = false

	if planned := cs.plan(player, crossfadeEndState(), 100, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade")
	}

	if !cs.armed {
		t.Error("plan reported success but did not arm")
	}
	if cs.autoMix {
		t.Error("a crossfade-only plan was marked as automix")
	}
	if cs.crossfadeFrames != 300 {
		t.Errorf("got %d crossfade frames, want 300 for 6s at 50fps", cs.crossfadeFrames)
	}
	if cs.transitionFrame != 8700 {
		t.Errorf("got transition frame %d, want 8700 (9000 total minus 300 crossfade)", cs.transitionFrame)
	}
	if cs.totalFrames != 9000 {
		t.Errorf("got total frames %d, want the effective end 9000", cs.totalFrames)
	}
	if cs.minUsableFrames != minUsableCrossfadeFrames {
		t.Errorf("got %d min usable frames, want %d", cs.minUsableFrames, minUsableCrossfadeFrames)
	}
	if cs.slideFrames != fallbackSlideFrames {
		t.Errorf("got %d slide frames, want the fallback %d without analysis", cs.slideFrames, fallbackSlideFrames)
	}
	if cs.nextSongID != q.Songs[1].ID {
		t.Errorf("got next song ID %d, want %d", cs.nextSongID, q.Songs[1].ID)
	}
	if cs.bStream != stream {
		t.Error("the planned state does not hold the stream that was started")
	}
	if !cs.fadeGains {
		t.Error("fadeGains was not taken from fade.crossfade")
	}
	if cs.guildID != guildID {
		t.Errorf("got guild ID %q, want %q", cs.guildID, guildID)
	}
	if cs.bitrate != 128000 {
		t.Errorf("got bitrate %d, want 128000", cs.bitrate)
	}
	if cs.processor == nil {
		t.Error("no transition processor was built")
	}
	if cs.loopBuffer != nil || cs.loopIndex != 0 {
		t.Error("the loop buffer was not reset")
	}
}

func TestCrossfadePlanTrimsTheSilentTailFromTheEffectiveEnd(t *testing.T) {
	guildID := "plantrim"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stubAudioStream(t)

	es := crossfadeEndState()
	es.SilentTailFrames = 500

	cs := newCrossfadeState()
	if planned := cs.plan(GetPlayer(guildID), es, 100, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade")
	}

	if cs.totalFrames != 8500 {
		t.Errorf("got total frames %d, want 8500 with 500 silent tail frames trimmed", cs.totalFrames)
	}
	if cs.transitionFrame != 8200 {
		t.Errorf("got transition frame %d, want 8200", cs.transitionFrame)
	}
}

func TestCrossfadePlanClampsTheTransitionAheadOfTheCurrentFrame(t *testing.T) {
	guildID := "planclamp"
	q := seedCrossfadeQueue(t, guildID)
	cacheNextStreamURL(t, guildID, q.Songs[1].ID, "https://example.invalid/next")
	stubAudioStream(t)

	cs := newCrossfadeState()
	if planned := cs.plan(GetPlayer(guildID), crossfadeEndState(), 8699, crossfadeFade(), false, 128000); !planned {
		t.Fatal("plan returned false, want an armed crossfade at the boundary")
	}

	if cs.transitionFrame != 8700 {
		t.Errorf("got transition frame %d, want 8700", cs.transitionFrame)
	}
}

func TestCrossfadePlanRefusesEveryGuard(t *testing.T) {
	guildID := "planguard"
	q := seedCrossfadeQueue(t, guildID)
	nextID := q.Songs[1].ID

	cases := []struct {
		name       string
		fade       fadeSettings
		endState   *ffmpeg.EndState
		sentFrames int
		cacheURL   string
		streamOK   bool
		mutate     func(cs *crossfadeState)
	}{
		{
			name:     "neither automix nor crossfade",
			fade:     fadeSettings{crossfadeSec: 6},
			cacheURL: "https://example.invalid/next",
			streamOK: true,
		},
		{
			name:     "already armed",
			fade:     crossfadeFade(),
			cacheURL: "https://example.invalid/next",
			streamOK: true,
			mutate:   func(cs *crossfadeState) { cs.armed = true },
		},
		{
			name:     "repeat single",
			fade:     fadeSettings{crossfade: true, crossfadeSec: 6, repeatMode: queue.RepeatSingle},
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

			cs := newCrossfadeState()
			if testCase.mutate != nil {
				testCase.mutate(cs)
			}
			wasArmed := cs.armed

			if planned := cs.plan(GetPlayer(guildID), es, testCase.sentFrames, testCase.fade, false, 128000); planned {
				t.Fatal("plan returned true, want a refusal")
			}
			if cs.armed != wasArmed {
				t.Error("a refused plan changed the armed flag")
			}
			if cs.bStream != nil {
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

	cs := newCrossfadeState()
	if planned := cs.plan(GetPlayer(guildID), crossfadeEndState(), 100, crossfadeFade(), false, 128000); planned {
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

	cs := newCrossfadeState()
	if planned := cs.plan(GetPlayer(guildID), crossfadeEndState(), 100, crossfadeFade(), false, 128000); planned {
		t.Error("plan returned true, want a refusal when the next song is live")
	}
}
