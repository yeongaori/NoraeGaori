package guild_test

import (
	"testing"

	"noraegaori/internal/guild"
	"noraegaori/tests/testutil/dbtest"
)

func TestLanguageRoundTrip(t *testing.T) {
	dbtest.Setup(t)

	guildID := "guild1"

	if err := guild.SetLanguage(guildID, "ko"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if lang, err := guild.GetLanguage(guildID); err != nil || lang != "ko" {
		t.Errorf("GetLanguage: want ko got %q (err %v)", lang, err)
	}
}

func TestPrefixRoundTrip(t *testing.T) {
	dbtest.Setup(t)

	guildID := "guild1"

	if err := guild.SetPrefix(guildID, "?"); err != nil {
		t.Fatalf("SetPrefix: %v", err)
	}
	if p, err := guild.GetPrefix(guildID); err != nil || p != "?" {
		t.Errorf("GetPrefix: want ? got %q (err %v)", p, err)
	}
}

func TestInvalidateCachesForgetsBothValues(t *testing.T) {
	dbtest.Setup(t)

	guildID := "guild1"

	if err := guild.SetLanguage(guildID, "ko"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if err := guild.SetPrefix(guildID, "?"); err != nil {
		t.Fatalf("SetPrefix: %v", err)
	}

	guild.InvalidateCaches(guildID)

	guild.HookLanguageCacheMux.RLock()
	_, langCached := (*guild.HookLanguageCacheLoaded)[guildID]
	guild.HookLanguageCacheMux.RUnlock()
	if langCached {
		t.Error("language cache should be empty after invalidation")
	}

	guild.HookPrefixCacheMux.RLock()
	_, prefixCached := (*guild.HookPrefixCacheLoaded)[guildID]
	guild.HookPrefixCacheMux.RUnlock()
	if prefixCached {
		t.Error("prefix cache should be empty after invalidation")
	}
}
