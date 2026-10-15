package player_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"noraegaori/internal/audio/analysis"
	"noraegaori/internal/player"
	"noraegaori/internal/queue"
	"noraegaori/tests/testutil"
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

func TestReadFloat32SamplesDecodesAcrossChunkBoundaries(t *testing.T) {
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

func TestReadFloat32SamplesStopsAtMaxBytes(t *testing.T) {
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

func TestReadFloat32SamplesReportsReadFailure(t *testing.T) {
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

func TestPreCacheNextStartsOneFetchPerSong(t *testing.T) {
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

func TestCachedPreCacheExpiresAfterItsTTL(t *testing.T) {
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
