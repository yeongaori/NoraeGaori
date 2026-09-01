package youtube

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func newTestPool(workerCount int, checkFn func(string) (bool, bool, error)) *AvailabilityPool {
	pool := newAvailabilityPool(workerCount)
	pool.checkFn = checkFn
	pool.retryDelay = 5 * time.Millisecond
	pool.maxRetryDelay = 20 * time.Millisecond
	pool.start()
	return pool
}

func testJobs(urls ...string) []BatchJob {
	jobs := make([]BatchJob, 0, len(urls))
	for i, url := range urls {
		jobs = append(jobs, BatchJob{URL: url, Index: i})
	}
	return jobs
}

func alwaysAvailable(string) (bool, bool, error) {
	return true, false, nil
}

func TestCheckBatchOrdersResultsByIndex(t *testing.T) {
	urls := []string{"a", "b", "c", "d", "e"}

	pool := newTestPool(4, func(url string) (bool, bool, error) {
		if url == "a" {
			time.Sleep(40 * time.Millisecond)
		}
		return true, false, nil
	})
	defer pool.Close()

	results := pool.CheckBatch(testJobs(urls...))

	if len(results) != len(urls) {
		t.Fatalf("got %d results, want %d", len(results), len(urls))
	}
	for i, url := range urls {
		if results[i].URL != url {
			t.Errorf("index %d: got %q, want %q", i, results[i].URL, url)
		}
		if !results[i].Available {
			t.Errorf("index %d: got unavailable, want available", i)
		}
	}
}

func TestCheckBatchEmptyReturnsImmediately(t *testing.T) {
	pool := newTestPool(2, alwaysAvailable)
	defer pool.Close()

	results := pool.CheckBatch(nil)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestRetryRecoversFromRateLimit(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	pool := newTestPool(2, func(url string) (bool, bool, error) {
		mu.Lock()
		calls++
		attempt := calls
		mu.Unlock()

		if attempt < 3 {
			return false, false, fmt.Errorf("HTTP Error 429: Too Many Requests")
		}
		return true, false, nil
	})
	defer pool.Close()

	results := pool.CheckBatch(testJobs("x"))

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !results[0].Available {
		t.Errorf("got %+v, want available after retries", results[0])
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Errorf("got %d attempts, want 3", calls)
	}
}

func TestRetryExhaustedReturnsError(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	pool := newTestPool(2, func(url string) (bool, bool, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return false, false, fmt.Errorf("connection reset by peer")
	})
	defer pool.Close()

	finished := make(chan []BatchResult, 1)
	go func() {
		finished <- pool.CheckBatch(testJobs("x"))
	}()

	select {
	case results := <-finished:
		if results[0].Available {
			t.Errorf("got available, want unavailable")
		}
		if results[0].Error == "" {
			t.Errorf("got empty error, want the underlying failure recorded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CheckBatch did not return after retries were exhausted")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != pool.maxRetries+1 {
		t.Errorf("got %d attempts, want %d", calls, pool.maxRetries+1)
	}
}

func TestBackoffDelayIsExponentialAndCapped(t *testing.T) {
	pool := newAvailabilityPool(1)
	pool.retryDelay = 1 * time.Second
	pool.maxRetryDelay = 10 * time.Second

	want := []time.Duration{1, 2, 4, 8, 10, 10}
	for retryCount, seconds := range want {
		got := pool.backoffDelay(retryCount)
		if got != seconds*time.Second {
			t.Errorf("retry %d: got %v, want %v", retryCount, got, seconds*time.Second)
		}
	}
}

func TestCloseWithRetriesInFlight(t *testing.T) {
	pool := newAvailabilityPool(4)
	pool.checkFn = func(string) (bool, bool, error) {
		return false, false, fmt.Errorf("HTTP Error 429: Too Many Requests")
	}
	pool.retryDelay = 500 * time.Millisecond
	pool.maxRetryDelay = 500 * time.Millisecond
	pool.start()

	batchResults := make(chan BatchResult, 8)
	pool.submit(testJobs("a", "b", "c", "d", "e", "f", "g", "h"), batchResults)

	time.Sleep(100 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		pool.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return while retries were sleeping")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	pool := newTestPool(2, alwaysAvailable)
	pool.Close()
	pool.Close()
}

func TestCheckBatchReturnsWhenPoolCloses(t *testing.T) {
	blocked := make(chan struct{})

	pool := newTestPool(1, func(string) (bool, bool, error) {
		<-blocked
		return true, false, nil
	})

	finished := make(chan []BatchResult, 1)
	go func() {
		finished <- pool.CheckBatch(testJobs("a", "b"))
	}()

	time.Sleep(100 * time.Millisecond)
	go pool.Close()

	select {
	case results := <-finished:
		if len(results) != 2 {
			t.Errorf("got %d result slots, want 2", len(results))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CheckBatch blocked after the pool was closed")
	}

	close(blocked)
}

func TestCheckBatchImmediateInvokesCallbackOncePerJob(t *testing.T) {
	pool := newTestPool(3, alwaysAvailable)
	defer pool.Close()

	jobs := testJobs("a", "b", "c", "d", "e", "f")
	seen := make(map[int]int)

	pool.CheckBatchImmediate(jobs, func(result BatchResult) {
		seen[result.Index]++
	})

	if len(seen) != len(jobs) {
		t.Fatalf("got %d distinct callbacks, want %d", len(seen), len(jobs))
	}
	for index, count := range seen {
		if count != 1 {
			t.Errorf("index %d: called %d times, want 1", index, count)
		}
	}
}
