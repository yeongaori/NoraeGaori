package youtube

import (
	"container/list"
	"sync"
	"time"
)

const (
	availabilityCacheTTL      = 10 * time.Minute
	availabilityCacheCapacity = 2048
)

type availabilityCacheEntry struct {
	key       string
	result    *AvailabilityResult
	timestamp time.Time
	element   *list.Element
}

var (
	availabilityCacheEntries = make(map[string]*availabilityCacheEntry)
	availabilityCacheOrder   = list.New()
	availabilityCacheMutex   sync.Mutex
)

func loadAvailability(key string) (*AvailabilityResult, bool) {
	availabilityCacheMutex.Lock()
	defer availabilityCacheMutex.Unlock()

	entry, exists := availabilityCacheEntries[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.timestamp) >= availabilityCacheTTL {
		availabilityCacheOrder.Remove(entry.element)
		delete(availabilityCacheEntries, key)
		return nil, false
	}

	availabilityCacheOrder.MoveToFront(entry.element)
	return entry.result, true
}

func saveAvailability(key string, result *AvailabilityResult) {
	availabilityCacheMutex.Lock()
	defer availabilityCacheMutex.Unlock()

	if entry, exists := availabilityCacheEntries[key]; exists {
		entry.result = result
		entry.timestamp = time.Now()
		availabilityCacheOrder.MoveToFront(entry.element)
		return
	}

	entry := &availabilityCacheEntry{
		key:       key,
		result:    result,
		timestamp: time.Now(),
	}
	entry.element = availabilityCacheOrder.PushFront(entry)
	availabilityCacheEntries[key] = entry

	for availabilityCacheOrder.Len() > availabilityCacheCapacity {
		oldest := availabilityCacheOrder.Back()
		availabilityCacheOrder.Remove(oldest)
		if evicted, ok := oldest.Value.(*availabilityCacheEntry); ok {
			delete(availabilityCacheEntries, evicted.key)
		}
	}
}

func resetAvailabilityCache() {
	availabilityCacheMutex.Lock()
	defer availabilityCacheMutex.Unlock()

	availabilityCacheEntries = make(map[string]*availabilityCacheEntry)
	availabilityCacheOrder = list.New()
}
