package guild

import (
	"testing"

	"noraegaori/internal/testutil/dbtest"
)

func TestLanguageRoundTrip(t *testing.T) {
	dbtest.Setup(t)

	guildID := "guild1"

	if err := SetLanguage(guildID, "ko"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if lang, err := GetLanguage(guildID); err != nil || lang != "ko" {
		t.Errorf("GetLanguage: want ko got %q (err %v)", lang, err)
	}
}

func TestPrefixRoundTrip(t *testing.T) {
	dbtest.Setup(t)

	guildID := "guild1"

	if err := SetPrefix(guildID, "?"); err != nil {
		t.Fatalf("SetPrefix: %v", err)
	}
	if p, err := GetPrefix(guildID); err != nil || p != "?" {
		t.Errorf("GetPrefix: want ? got %q (err %v)", p, err)
	}
}

func TestInvalidateCachesForgetsBothValues(t *testing.T) {
	dbtest.Setup(t)

	guildID := "guild1"

	if err := SetLanguage(guildID, "ko"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if err := SetPrefix(guildID, "?"); err != nil {
		t.Fatalf("SetPrefix: %v", err)
	}

	InvalidateCaches(guildID)

	languageCacheMux.RLock()
	_, langCached := languageCacheLoaded[guildID]
	languageCacheMux.RUnlock()
	if langCached {
		t.Error("language cache should be empty after invalidation")
	}

	prefixCacheMux.RLock()
	_, prefixCached := prefixCacheLoaded[guildID]
	prefixCacheMux.RUnlock()
	if prefixCached {
		t.Error("prefix cache should be empty after invalidation")
	}
}
