package lockmap

import (
	"sync"
	"time"
)

type Map struct {
	mutex   sync.Mutex
	entries map[string]*entry
}

type entry struct {
	semaphore  chan struct{}
	references int
}

func (lockMap *Map) Acquire(key string) func() {
	held := lockMap.retain(key)
	held.semaphore <- struct{}{}

	return lockMap.createRelease(key, held)
}

func (lockMap *Map) AcquireWithTimeout(key string, timeout time.Duration) (func(), bool) {
	held := lockMap.retain(key)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case held.semaphore <- struct{}{}:
		return lockMap.createRelease(key, held), true
	case <-timer.C:
		lockMap.releaseReference(key, held)
		return nil, false
	}
}

func (lockMap *Map) CountKeys() int {
	lockMap.mutex.Lock()
	defer lockMap.mutex.Unlock()

	return len(lockMap.entries)
}

func (lockMap *Map) retain(key string) *entry {
	lockMap.mutex.Lock()
	defer lockMap.mutex.Unlock()

	if lockMap.entries == nil {
		lockMap.entries = make(map[string]*entry)
	}

	held, exists := lockMap.entries[key]
	if !exists {
		held = &entry{semaphore: make(chan struct{}, 1)}
		lockMap.entries[key] = held
	}
	held.references++

	return held
}

func (lockMap *Map) releaseReference(key string, held *entry) {
	lockMap.mutex.Lock()
	defer lockMap.mutex.Unlock()

	held.references--
	if held.references == 0 {
		delete(lockMap.entries, key)
	}
}

func (lockMap *Map) createRelease(key string, held *entry) func() {
	return sync.OnceFunc(func() {
		<-held.semaphore
		lockMap.releaseReference(key, held)
	})
}
