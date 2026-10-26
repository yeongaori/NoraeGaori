//go:build testhooks

package guild

var HookGuildLocks = &guildLocks
var HookLanguageCacheLoaded = &languageCache.isLoaded
var HookLanguageCacheMux = &languageCache.mu
var HookPrefixCacheLoaded = &prefixCache.isLoaded
var HookPrefixCacheMux = &prefixCache.mu
