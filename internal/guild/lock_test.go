package guild

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestGuildLockSerializesSameGuild(t *testing.T) {
	var inFlight atomic.Int32
	var counter int

	var waitGroup sync.WaitGroup
	for index := 0; index < 50; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			release := AcquireLock("guild1")
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
}

func TestGuildLocksAreReaped(t *testing.T) {
	var waitGroup sync.WaitGroup
	for index := 0; index < 500; index++ {
		waitGroup.Add(1)
		go func(guildID string) {
			defer waitGroup.Done()

			release := AcquireLock(guildID)
			release()
		}(fmt.Sprintf("guild-%d", index))
	}
	waitGroup.Wait()

	if keys := guildLocks.CountKeys(); keys != 0 {
		t.Errorf("CountKeys() = %d, want 0", keys)
	}
}
