package youtube_test

import (
	"fmt"
	"testing"
	"time"

	"noraegaori/internal/youtube"
)

func TestAvailabilityCacheEvictsTheOldestEntryPastCapacity(t *testing.T) {
	youtube.HookResetAvailabilityCache()
	t.Cleanup(youtube.HookResetAvailabilityCache)

	oldest := "https://www.youtube.com/watch?v=oldestVideo"
	youtube.HookSaveAvailability(oldest, &youtube.AvailabilityResult{Available: true})

	for index := 0; index < youtube.HookAvailabilityCacheCapacity; index++ {
		youtube.HookSaveAvailability(fmt.Sprintf("https://www.youtube.com/watch?v=filler%d", index), &youtube.AvailabilityResult{Available: true})
	}

	if _, ok := youtube.HookLoadAvailability(oldest); ok {
		t.Error("the oldest entry survived, so the cache is not bounded")
	}

	newest := fmt.Sprintf("https://www.youtube.com/watch?v=filler%d", youtube.HookAvailabilityCacheCapacity-1)
	if _, ok := youtube.HookLoadAvailability(newest); !ok {
		t.Error("the newest entry was evicted, want it kept")
	}

	if got := (*youtube.HookAvailabilityCacheOrder).Len(); got > youtube.HookAvailabilityCacheCapacity {
		t.Errorf("cache holds %d entries, want at most %d", got, youtube.HookAvailabilityCacheCapacity)
	}
}

func TestAvailabilityCacheKeepsAReadEntryAlive(t *testing.T) {
	youtube.HookResetAvailabilityCache()
	t.Cleanup(youtube.HookResetAvailabilityCache)

	kept := "https://www.youtube.com/watch?v=keptVideo01"
	youtube.HookSaveAvailability(kept, &youtube.AvailabilityResult{Available: true})

	for index := 0; index < youtube.HookAvailabilityCacheCapacity-1; index++ {
		youtube.HookSaveAvailability(fmt.Sprintf("https://www.youtube.com/watch?v=filler%d", index), &youtube.AvailabilityResult{Available: true})
		if _, ok := youtube.HookLoadAvailability(kept); !ok {
			t.Fatalf("the entry was evicted after %d insertions despite being read", index+1)
		}
	}
}

func TestAvailabilityCacheMissesExpiredEntries(t *testing.T) {
	youtube.HookResetAvailabilityCache()
	t.Cleanup(youtube.HookResetAvailabilityCache)

	expired := "https://www.youtube.com/watch?v=expiredVid"
	youtube.HookSaveAvailability(expired, &youtube.AvailabilityResult{Available: true})

	youtube.HookAvailabilityCacheMutex.Lock()
	*(*youtube.HookAvailabilityCacheEntries)[expired].HookTimestamp() = time.Now().Add(-youtube.HookAvailabilityCacheTTL - time.Second)
	youtube.HookAvailabilityCacheMutex.Unlock()

	if _, ok := youtube.HookLoadAvailability(expired); ok {
		t.Error("an expired entry was served, want a miss")
	}

	if _, exists := (*youtube.HookAvailabilityCacheEntries)[expired]; exists {
		t.Error("the expired entry was left behind, want it dropped on read")
	}
}

func TestAvailabilityCacheRefreshesAnExistingKeyInPlace(t *testing.T) {
	youtube.HookResetAvailabilityCache()
	t.Cleanup(youtube.HookResetAvailabilityCache)

	refreshed := "https://www.youtube.com/watch?v=refreshVid"
	youtube.HookSaveAvailability(refreshed, &youtube.AvailabilityResult{Available: false, Error: "stale"})
	youtube.HookSaveAvailability(refreshed, &youtube.AvailabilityResult{Available: true, IsLive: true})

	if got := (*youtube.HookAvailabilityCacheOrder).Len(); got != 1 {
		t.Errorf("cache holds %d entries, want 1 after re-saving the same key", got)
	}

	cached, ok := youtube.HookLoadAvailability(refreshed)
	if !ok {
		t.Fatal("the refreshed entry is missing")
	}
	if !cached.Available || !cached.IsLive || cached.Error != "" {
		t.Errorf("cached = %+v, want the newer result", cached)
	}
}
