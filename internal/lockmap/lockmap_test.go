package lockmap

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireSerializesSameKey(t *testing.T) {
	var locks Map
	var inFlight atomic.Int32
	var counter int

	var waitGroup sync.WaitGroup
	for index := 0; index < 50; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			release := locks.Acquire("guild")
			defer release()

			if inFlight.Add(1) != 1 {
				t.Error("more than one holder inside the critical section")
			}
			counter++
			inFlight.Add(-1)
		}()
	}
	waitGroup.Wait()

	if counter != 50 {
		t.Errorf("counter = %d, want 50", counter)
	}
	if keys := locks.CountKeys(); keys != 0 {
		t.Errorf("CountKeys() = %d, want 0", keys)
	}
}

func TestAcquireAllowsDifferentKeysConcurrently(t *testing.T) {
	var locks Map

	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})

	go func() {
		release := locks.Acquire("first")
		defer release()

		close(firstEntered)
		<-secondEntered
	}()

	release := locks.Acquire("second")
	defer release()

	close(secondEntered)

	select {
	case <-firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("holders of different keys blocked each other")
	}
}

func TestAcquireWithTimeoutFailsWhileHeld(t *testing.T) {
	var locks Map

	release := locks.Acquire("guild")
	defer release()

	blocked, isAcquired := locks.AcquireWithTimeout("guild", 20*time.Millisecond)
	if isAcquired {
		t.Fatal("acquired a lock that was already held")
	}
	if blocked != nil {
		t.Error("failed acquisition returned a non-nil release")
	}
}

func TestAcquireWithTimeoutDoesNotStrandTheLock(t *testing.T) {
	var locks Map

	release := locks.Acquire("guild")

	if _, isAcquired := locks.AcquireWithTimeout("guild", 20*time.Millisecond); isAcquired {
		t.Fatal("acquired a lock that was already held")
	}

	release()

	next, isAcquired := locks.AcquireWithTimeout("guild", time.Second)
	if !isAcquired {
		t.Fatal("lock was stranded after a timed-out acquisition")
	}
	next()

	if keys := locks.CountKeys(); keys != 0 {
		t.Errorf("CountKeys() = %d, want 0", keys)
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	var locks Map

	release := locks.Acquire("guild")
	release()
	release()

	done := make(chan struct{})
	go func() {
		next := locks.Acquire("guild")
		next()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a doubled release corrupted the lock")
	}

	if keys := locks.CountKeys(); keys != 0 {
		t.Errorf("CountKeys() = %d, want 0", keys)
	}
}

func TestEntriesAreReapedWhenIdle(t *testing.T) {
	var locks Map

	var waitGroup sync.WaitGroup
	for index := 0; index < 1000; index++ {
		waitGroup.Add(1)
		go func(key string) {
			defer waitGroup.Done()

			release := locks.Acquire(key)
			release()
		}(fmt.Sprintf("guild-%d", index))
	}
	waitGroup.Wait()

	if keys := locks.CountKeys(); keys != 0 {
		t.Errorf("CountKeys() = %d, want 0", keys)
	}
}

func TestTimeoutRacesReleaseUnderLoad(t *testing.T) {
	var locks Map

	var waitGroup sync.WaitGroup
	for index := 0; index < 50; index++ {
		waitGroup.Add(1)
		go func(shouldTimeout bool) {
			defer waitGroup.Done()

			if shouldTimeout {
				if release, isAcquired := locks.AcquireWithTimeout("guild", time.Millisecond); isAcquired {
					release()
				}
				return
			}

			release := locks.Acquire("guild")
			release()
		}(index%2 == 0)
	}
	waitGroup.Wait()

	if keys := locks.CountKeys(); keys != 0 {
		t.Errorf("CountKeys() = %d, want 0", keys)
	}
}
