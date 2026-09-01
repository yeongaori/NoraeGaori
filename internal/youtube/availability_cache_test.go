package youtube

import (
	"fmt"
	"testing"
	"time"
)

func TestAvailabilityCacheEvictsTheOldestEntryPastCapacity(t *testing.T) {
	resetAvailabilityCache()
	t.Cleanup(resetAvailabilityCache)

	oldest := "https://www.youtube.com/watch?v=oldestVideo"
	saveAvailability(oldest, &AvailabilityResult{Available: true})

	for index := 0; index < availabilityCacheCapacity; index++ {
		saveAvailability(fmt.Sprintf("https://www.youtube.com/watch?v=filler%d", index), &AvailabilityResult{Available: true})
	}

	if _, ok := loadAvailability(oldest); ok {
		t.Error("the oldest entry survived, so the cache is not bounded")
	}

	newest := fmt.Sprintf("https://www.youtube.com/watch?v=filler%d", availabilityCacheCapacity-1)
	if _, ok := loadAvailability(newest); !ok {
		t.Error("the newest entry was evicted, want it kept")
	}

	if got := availabilityCacheOrder.Len(); got > availabilityCacheCapacity {
		t.Errorf("cache holds %d entries, want at most %d", got, availabilityCacheCapacity)
	}
}

func TestAvailabilityCacheKeepsAReadEntryAlive(t *testing.T) {
	resetAvailabilityCache()
	t.Cleanup(resetAvailabilityCache)

	kept := "https://www.youtube.com/watch?v=keptVideo01"
	saveAvailability(kept, &AvailabilityResult{Available: true})

	for index := 0; index < availabilityCacheCapacity-1; index++ {
		saveAvailability(fmt.Sprintf("https://www.youtube.com/watch?v=filler%d", index), &AvailabilityResult{Available: true})
		if _, ok := loadAvailability(kept); !ok {
			t.Fatalf("the entry was evicted after %d insertions despite being read", index+1)
		}
	}
}

func TestAvailabilityCacheMissesExpiredEntries(t *testing.T) {
	resetAvailabilityCache()
	t.Cleanup(resetAvailabilityCache)

	expired := "https://www.youtube.com/watch?v=expiredVid"
	saveAvailability(expired, &AvailabilityResult{Available: true})

	availabilityCacheMutex.Lock()
	availabilityCacheEntries[expired].timestamp = time.Now().Add(-availabilityCacheTTL - time.Second)
	availabilityCacheMutex.Unlock()

	if _, ok := loadAvailability(expired); ok {
		t.Error("an expired entry was served, want a miss")
	}

	if _, exists := availabilityCacheEntries[expired]; exists {
		t.Error("the expired entry was left behind, want it dropped on read")
	}
}

func TestAvailabilityCacheRefreshesAnExistingKeyInPlace(t *testing.T) {
	resetAvailabilityCache()
	t.Cleanup(resetAvailabilityCache)

	refreshed := "https://www.youtube.com/watch?v=refreshVid"
	saveAvailability(refreshed, &AvailabilityResult{Available: false, Error: "stale"})
	saveAvailability(refreshed, &AvailabilityResult{Available: true, IsLive: true})

	if got := availabilityCacheOrder.Len(); got != 1 {
		t.Errorf("cache holds %d entries, want 1 after re-saving the same key", got)
	}

	cached, ok := loadAvailability(refreshed)
	if !ok {
		t.Fatal("the refreshed entry is missing")
	}
	if !cached.Available || !cached.IsLive || cached.Error != "" {
		t.Errorf("cached = %+v, want the newer result", cached)
	}
}
