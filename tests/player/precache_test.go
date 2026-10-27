package player_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
	"noraegaori/tests/testutil/logtest"
)

type chunkedReader struct {
	data    []byte
	maxRead int
	rng     *rand.Rand
	failure error
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.failure != nil {
			return 0, r.failure
		}
		return 0, io.EOF
	}

	n := 1 + r.rng.Intn(r.maxRead)
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.data) {
		n = len(r.data)
	}
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

func encodeFloat32LE(values []float32, trailingBytes int) []byte {
	var raw bytes.Buffer
	for _, value := range values {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], math.Float32bits(value))
		raw.Write(word[:])
	}
	raw.Write(make([]byte, trailingBytes))
	return raw.Bytes()
}

func TestReadFloat32SamplesChunks(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	for _, count := range []int{0, 1, 2, 100, 4097} {
		for _, trailing := range []int{0, 1, 2, 3} {
			want := make([]float32, count)
			for i := range want {
				want[i] = rng.Float32()*2 - 1
			}

			raw := encodeFloat32LE(want, trailing)
			reader := &chunkedReader{data: raw, maxRead: 13, rng: rand.New(rand.NewSource(9))}

			got, err := player.HookReadFloat32Samples(reader, int64(len(raw)))
			if err != nil {
				t.Fatalf("%d samples, %d trailing bytes: %v", count, trailing, err)
			}
			if len(got) != len(want) {
				t.Fatalf("%d samples, %d trailing bytes: len = %d, want %d",
					count, trailing, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%d samples, %d trailing bytes: sample[%d] = %v, want %v",
						count, trailing, i, got[i], want[i])
				}
			}
		}
	}
}

func TestReadFloat32SamplesMaxBytes(t *testing.T) {
	want := make([]float32, 64)
	for i := range want {
		want[i] = float32(i)
	}
	raw := encodeFloat32LE(want, 0)

	reader := &chunkedReader{data: raw, maxRead: 7, rng: rand.New(rand.NewSource(3))}
	got, err := player.HookReadFloat32Samples(reader, 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
	for i, sample := range got {
		if sample != want[i] {
			t.Fatalf("sample[%d] = %v, want %v", i, sample, want[i])
		}
	}
}

func TestReadFloat32SamplesFailure(t *testing.T) {
	failure := errors.New("pipe broke")
	raw := encodeFloat32LE([]float32{1, 2, 3}, 0)

	reader := &chunkedReader{data: raw, maxRead: 4, rng: rand.New(rand.NewSource(5)), failure: failure}

	samples, err := player.HookReadFloat32Samples(reader, int64(len(raw))+16)
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
	}
	if samples != nil {
		t.Errorf("samples = %v, want nil on failure", samples)
	}
}

func TestPreCacheNextOneFetch(t *testing.T) {
	var mu sync.Mutex
	fetches := make(map[string]int)
	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		mu.Lock()
		fetches[url]++
		mu.Unlock()
		return url, nil
	})
	finished := make(chan struct{}, 64)
	testutil.Swap(t, player.HookStartAnalysisBackfill, func(string, int) {
		finished <- struct{}{}
	})

	for round := range 20 {
		guildID := fmt.Sprintf("precacheonefetch%d", round)
		setupPlayerDB(t, guildID, 2)
		t.Cleanup(func() { player.ClearPreCache(guildID) })

		q, err := queue.GetQueue(guildID, true)
		if err != nil || q == nil || len(q.Songs) != 2 {
			t.Fatalf("queue not ready: %v", err)
		}
		next := q.Songs[1]

		player.PreCacheNext(guildID, 128000)
		if player.GetPreCache(guildID, next.ID) == nil {
			t.Fatalf("round %d: got no pre-cache entry for %s when PreCacheNext returned, want it claimed before the fetch", round, next.Title)
		}
		player.PreCacheNext(guildID, 128000)

		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatalf("round %d: the pre-cache of %s did not finish", round, next.Title)
		}

		mu.Lock()
		got := fetches[next.URL]
		mu.Unlock()
		if got != 1 {
			t.Fatalf("round %d: got %d stream URL fetches for %s, want 1 per song", round, got, next.Title)
		}
	}
}

func TestGetPreCacheExpires(t *testing.T) {
	const guildID = "precacheexpired"
	const expiredURL = "https://example.invalid/expired"
	const freshURL = "https://example.invalid/fresh"
	t.Cleanup(func() { player.ClearPreCache(guildID) })

	player.HookPreCacheStoreMu.Lock()
	(*player.HookPreCacheStore)[guildID+"_1"] = &player.PreCache{
		StreamURL: expiredURL,
		SongID:    1,
		Timestamp: time.Now().Add(-61 * time.Minute),
		Analysis:  &analysis.TrackAnalysis{BPM: 120},
	}
	(*player.HookPreCacheStore)[guildID+"_2"] = &player.PreCache{
		StreamURL: freshURL,
		SongID:    2,
		Timestamp: time.Now().Add(-59 * time.Minute),
		Analysis:  &analysis.TrackAnalysis{BPM: 120},
	}
	player.HookPreCacheStoreMu.Unlock()

	if player.GetPreCache(guildID, 1) != nil {
		t.Error("got a pre-cache entry 61 minutes old, want it expired")
	}
	if got := player.GetCachedStreamURL(guildID, 1); got != "" {
		t.Errorf("got stream URL %q from an expired entry, want none", got)
	}
	if player.GetCachedAnalysis(guildID, 1) != nil {
		t.Error("got an analysis from an expired entry, want none")
	}
	if got := player.GetCachedStreamURL(guildID, 2); got != freshURL {
		t.Errorf("got stream URL %q from a 59-minute-old entry, want %q", got, freshURL)
	}
}

func runLoggedPreCache(t *testing.T, guildID string, prepare func(next *queue.Song), analyze func() (*analysis.TrackAnalysis, error)) string {
	t.Helper()

	readLog := logtest.Capture(t)
	setupPlayerDB(t, guildID, 2)
	enableAutoMix(t, guildID)
	t.Cleanup(func() { player.ClearPreCache(guildID) })
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 2 {
		t.Fatalf("queue not ready: %v", err)
	}
	next := q.Songs[1]
	if prepare != nil {
		prepare(next)
	}

	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		time.Sleep(400 * time.Millisecond)
		return url, nil
	})
	testutil.Swap(t, player.HookAnalyzeSegment, func(context.Context, string, float64, int) (*analysis.TrackAnalysis, error) {
		return analyze()
	})
	finished := make(chan struct{}, 1)
	testutil.Swap(t, player.HookStartAnalysisBackfill, func(string, int) {
		finished <- struct{}{}
	})

	player.PreCacheNext(guildID, 128000)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatalf("the pre-cache of %s did not finish", next.Title)
	}
	return readLog()
}

func TestPreCacheLogsTiming(t *testing.T) {
	log := runLoggedPreCache(t, "precachetimes", nil, func() (*analysis.TrackAnalysis, error) {
		time.Sleep(200 * time.Millisecond)
		return &analysis.TrackAnalysis{BPM: 120, PeriodSec: 0.5, Duration: 75}, nil
	})

	if fetched := loggedSeconds(t, log, `Cached stream URL for: Song 1 \(fetched in ([0-9.]+)s\)`); fetched < 0.4 || fetched >= 0.7 {
		t.Errorf("got fetched in %.1fs, want 0.4s to 0.7s", fetched)
	}
	if analyzed := loggedSeconds(t, log, `Analyzed head for: Song 1 \(.*, analyzed in ([0-9.]+)s\)`); analyzed < 0.2 || analyzed >= 0.5 {
		t.Errorf("got analyzed in %.1fs, want 0.2s to 0.5s", analyzed)
	}
}

func TestPreCacheLogsReusedHead(t *testing.T) {
	analyses := 0
	storeHead := func(next *queue.Song) {
		if err := analysis.SaveTrackAnalysis(next.URL, analysis.SegmentHead, &analysis.TrackAnalysis{BPM: 120, PeriodSec: 0.5, Duration: 75}); err != nil {
			t.Fatal(err)
		}
	}
	log := runLoggedPreCache(t, "precachereused", storeHead, func() (*analysis.TrackAnalysis, error) {
		analyses++
		return &analysis.TrackAnalysis{BPM: 120, PeriodSec: 0.5, Duration: 75}, nil
	})

	if got := regexp.MustCompile(`Analyzed head for: Song 1 \(.*\)`).FindString(log); !strings.HasSuffix(got, ", reused)") {
		t.Errorf("got head line %q, want it marked reused with no time", got)
	}
	if analyses != 0 {
		t.Errorf("got %d analyses, want 0 for a stored head", analyses)
	}
}

func TestPreCacheLogsFailedTiming(t *testing.T) {
	log := runLoggedPreCache(t, "precachefailed", nil, func() (*analysis.TrackAnalysis, error) {
		time.Sleep(200 * time.Millisecond)
		return nil, io.ErrUnexpectedEOF
	})

	if failed := loggedSeconds(t, log, `Head analysis failed for Song 1 after ([0-9.]+)s: `); failed < 0.2 || failed >= 0.5 {
		t.Errorf("got failed after %.1fs, want 0.2s to 0.5s", failed)
	}
}

func TestPreCacheDropsCancelled(t *testing.T) {
	const guildID = "precachedropped"
	readLog := logtest.Capture(t)
	setupPlayerDB(t, guildID, 2)
	enableAutoMix(t, guildID)
	t.Cleanup(func() { player.ClearPreCache(guildID) })
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 2 {
		t.Fatalf("queue not ready: %v", err)
	}
	next := q.Songs[1]

	fetchStarted := make(chan struct{})
	release := make(chan struct{})
	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		close(fetchStarted)
		<-release
		return url, nil
	})
	analyses := 0
	testutil.Swap(t, player.HookAnalyzeSegment, func(context.Context, string, float64, int) (*analysis.TrackAnalysis, error) {
		analyses++
		return &analysis.TrackAnalysis{BPM: 120, PeriodSec: 0.5, Duration: 75}, nil
	})
	finished := make(chan struct{}, 1)
	testutil.Swap(t, player.HookStartAnalysisBackfill, func(string, int) {
		finished <- struct{}{}
	})

	player.PreCacheNext(guildID, 128000)
	select {
	case <-fetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatalf("the pre-cache of %s did not start fetching", next.Title)
	}
	player.InvalidatePreCaches(guildID, []int{next.ID})
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatalf("the pre-cache of %s did not finish", next.Title)
	}

	if player.GetPreCache(guildID, next.ID) != nil {
		t.Errorf("got a pre-cache entry for %s after it was cancelled, want none", next.Title)
	}
	if analyses != 0 {
		t.Errorf("got %d analyses, want 0 for a cancelled pre-cache", analyses)
	}
	if got := countLogLines(readLog(), "Dropped the stream URL for: "+next.Title); got != 1 {
		t.Errorf("got %d dropped stream URL lines, want 1", got)
	}
}

func runPreCacheAndWait(t *testing.T, guildID string, next *queue.Song, finished <-chan struct{}) {
	t.Helper()

	player.PreCacheNext(guildID, 128000)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatalf("the pre-cache of %s did not finish", next.Title)
	}
}

func backdateFailure(guildID string, songID int, age time.Duration) {
	player.HookPreCacheStoreMu.Lock()
	(*player.HookPreCacheStore)[fmt.Sprintf("%s_%d", guildID, songID)].FailedAt = time.Now().Add(-age)
	player.HookPreCacheStoreMu.Unlock()
}

func TestPreCacheRetriesFailure(t *testing.T) {
	const guildID = "precacheretry"
	setupPlayerDB(t, guildID, 2)
	t.Cleanup(func() { player.ClearPreCache(guildID) })
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 2 {
		t.Fatalf("queue not ready: %v", err)
	}
	next := q.Songs[1]

	fetches := 0
	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		fetches++
		if fetches == 1 {
			return "", io.ErrUnexpectedEOF
		}
		return url, nil
	})
	finished := make(chan struct{}, 1)
	testutil.Swap(t, player.HookStartAnalysisBackfill, func(string, int) {
		finished <- struct{}{}
	})

	runPreCacheAndWait(t, guildID, next, finished)
	if got := player.GetCachedStreamURL(guildID, next.ID); got != "" {
		t.Errorf("got stream URL %q after a failed fetch, want none", got)
	}

	failed := player.GetPreCache(guildID, next.ID)
	player.HookPreCacheStoreMu.Lock()
	isFailureRecorded := failed != nil && !failed.FailedAt.IsZero()
	player.HookPreCacheStoreMu.Unlock()
	if !isFailureRecorded {
		t.Error("got no failure time after a failed fetch, want one")
	}

	player.PreCacheNext(guildID, 128000)
	if player.GetPreCache(guildID, next.ID) != failed {
		t.Error("got a retry right after the failure, want none before 30s")
	}

	backdateFailure(guildID, next.ID, 29*time.Second)
	player.PreCacheNext(guildID, 128000)
	if player.GetPreCache(guildID, next.ID) != failed {
		t.Error("got a retry 29s after the failure, want none before 30s")
	}

	backdateFailure(guildID, next.ID, 31*time.Second)
	runPreCacheAndWait(t, guildID, next, finished)
	if got := player.GetCachedStreamURL(guildID, next.ID); got != next.URL {
		t.Errorf("got stream URL %q 31s after the failure, want %q", got, next.URL)
	}

	cached := player.GetPreCache(guildID, next.ID)
	player.PreCacheNext(guildID, 128000)
	if player.GetPreCache(guildID, next.ID) != cached {
		t.Error("got a new pre-cache for a cached song, want it kept")
	}
}

func TestPreCacheStaleFailure(t *testing.T) {
	const guildID = "precachestalefailure"
	setupPlayerDB(t, guildID, 2)
	t.Cleanup(func() { player.ClearPreCache(guildID) })
	q, err := queue.GetQueue(guildID, true)
	if err != nil || q == nil || len(q.Songs) != 2 {
		t.Fatalf("queue not ready: %v", err)
	}
	next := q.Songs[1]

	staleStarted := make(chan struct{})
	releaseStale := make(chan struct{})
	var fetches atomic.Int32
	testutil.Swap(t, player.HookFetchStreamURL, func(url string, _ bool, _ int) (string, error) {
		if fetches.Add(1) == 1 {
			close(staleStarted)
			<-releaseStale
			return "", io.ErrUnexpectedEOF
		}
		return url, nil
	})
	finished := make(chan struct{}, 2)
	testutil.Swap(t, player.HookStartAnalysisBackfill, func(string, int) {
		finished <- struct{}{}
	})
	waitForFinish := func() {
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatalf("the pre-cache of %s did not finish", next.Title)
		}
	}

	player.PreCacheNext(guildID, 128000)
	select {
	case <-staleStarted:
	case <-time.After(5 * time.Second):
		t.Fatalf("the pre-cache of %s did not start fetching", next.Title)
	}
	player.InvalidatePreCaches(guildID, []int{next.ID})
	player.PreCacheNext(guildID, 128000)
	waitForFinish()
	close(releaseStale)
	waitForFinish()

	newer := player.GetPreCache(guildID, next.ID)
	player.HookPreCacheStoreMu.Lock()
	isMarked := newer != nil && !newer.FailedAt.IsZero()
	player.HookPreCacheStoreMu.Unlock()
	if isMarked {
		t.Errorf("got a failure time on the newer pre-cache of %s, want none", next.Title)
	}
}
