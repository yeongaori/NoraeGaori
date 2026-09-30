//go:build testhooks

package queue

import "database/sql"

var HookCache = &cache
var HookCacheMux = &cacheMux
var HookDefaultGuildSettingsRow = defaultGuildSettingsRow
var HookLoadQueueFromDB = loadQueueFromDB
var HookSettingsCache = &settingsCache
var HookSettingsGeneration = settingsGeneration
var HookStoreGuildSettings = storeGuildSettings

func (g *guildSettingsRow) HookAutoMix() *bool {
	return &g.autoMix
}

func (g *guildSettingsRow) HookAutoMixBeats() *int {
	return &g.autoMixBeats
}

func (g *guildSettingsRow) HookCrossfade() *bool {
	return &g.crossfade
}

func (g *guildSettingsRow) HookCrossfadeDuration() *float64 {
	return &g.crossfadeDuration
}

func (g *guildSettingsRow) HookFadeIn() *bool {
	return &g.fadeIn
}

func (g *guildSettingsRow) HookFadeInDuration() *float64 {
	return &g.fadeInDuration
}

func (g *guildSettingsRow) HookFadeOnStop() *bool {
	return &g.fadeOnStop
}

func (g *guildSettingsRow) HookFadeOut() *bool {
	return &g.fadeOut
}

func (g *guildSettingsRow) HookFadeOutDuration() *float64 {
	return &g.fadeOutDuration
}

func (g *guildSettingsRow) HookNormalization() *bool {
	return &g.normalization
}

func (g *guildSettingsRow) HookRepeat() *int {
	return &g.repeat
}

func (g *guildSettingsRow) HookShowStartedTrack() *bool {
	return &g.showStartedTrack
}

func (g *guildSettingsRow) HookSponsorBlock() *bool {
	return &g.sponsorBlock
}

func (g *guildSettingsRow) HookTrimSilence() *bool {
	return &g.trimSilence
}

func (g *guildSettingsRow) HookVolume() *float64 {
	return &g.volume
}

func (q *queueCache) HookDb() **sql.DB {
	return &q.db
}

func (s *settingsCacheEntry) HookDb() **sql.DB {
	return &s.db
}
