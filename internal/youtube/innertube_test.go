package youtube

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestGetInnertubeClientInitializesExactlyOnce(t *testing.T) {
	previousInit := innertubeInit
	previousClient := innertubeClient
	previousOnce := innertubeOnce

	t.Cleanup(func() {
		innertubeInit = previousInit
		innertubeClient = previousClient
		innertubeOnce = previousOnce
	})

	var calls int64
	innertubeClient = nil
	innertubeOnce = &sync.Once{}
	innertubeInit = func() {
		atomic.AddInt64(&calls, 1)
		innertubeClient = &InnertubeClient{clientName: "TEST"}
	}

	const goroutines = 32
	seen := make([]*InnertubeClient, goroutines)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			seen[index] = getInnertubeClient()
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("the initializer ran %d times, want exactly 1", got)
	}

	for index, client := range seen {
		if client == nil {
			t.Fatalf("goroutine %d observed a nil client", index)
		}
		if client != seen[0] {
			t.Errorf("goroutine %d observed a different client instance", index)
		}
	}
}

func TestExtractVideoIDHandlesEveryURLShapeTheBotStores(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"watch", "https://www.youtube.com/watch?v=ezXluhqaqfI", "ezXluhqaqfI"},
		{"music", "https://music.youtube.com/watch?v=SGmfjVIGUcY", "SGmfjVIGUcY"},
		{"short", "https://youtu.be/3pot1k7jNMk", "3pot1k7jNMk"},
		{"short with tracking", "https://youtu.be/r2ko422xW0w?si=FbAFmfYCH0ccc1EW", "r2ko422xW0w"},
		{"watch in a playlist", "https://www.youtube.com/watch?v=D3boQlSnHCg&list=PLabc", "D3boQlSnHCg"},
		{"embed", "https://www.youtube.com/embed/tXHXkDqn_Ic", "tXHXkDqn_Ic"},
		{"not a video URL", "https://example.com/song.mp3", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractVideoID(test.url); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}
