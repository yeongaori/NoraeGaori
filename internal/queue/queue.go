package queue

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"noraegaori/internal/config"
	"noraegaori/internal/database"
	"noraegaori/internal/guild"
	"noraegaori/internal/logger"
)

const (
	RepeatOff    = 0
	RepeatAll    = 1
	RepeatSingle = 2
)

type Queue struct {
	GuildID            string
	TextChannelID      string
	VoiceChannelID     string
	Songs              []*Song
	Volume             float64
	RepeatMode         int
	SponsorBlock       bool
	ShowStartedTrack   bool
	Normalization      bool
	Paused             bool
	Playing            bool
	Loading            bool
	FadeIn             bool
	FadeOut            bool
	AutoMix            bool
	FadeOnStop         bool
	FadeInDuration     float64
	FadeOutDuration    float64
	AutoMixBeats       int
	Crossfade          bool
	CrossfadeDuration  float64
	TrimSilence        bool
	AutoMixStyleVolume string
	AutoMixStyleEQ     string
	AutoMixStyleFilter string
	AutoMixStyleEffect string
	AutoMixStyleLoop   string
}

type queueCache struct {
	queue     *Queue
	timestamp time.Time
}

var (
	cache    = make(map[string]*queueCache)
	cacheMux sync.RWMutex
	cacheTTL = 30 * time.Second
)

func GetQueue(guildID string, forceRefresh bool) (*Queue, error) {
	lock := guild.AcquireLock(guildID)
	lock.Lock()
	defer lock.Unlock()

	if !forceRefresh {
		cacheMux.RLock()
		cached, exists := cache[guildID]
		cacheMux.RUnlock()

		if exists && time.Since(cached.timestamp) < cacheTTL {
			logger.Debugf("Using cached queue for guild: %s", guildID)
			return cached.queue, nil
		}
	}

	queue, err := loadQueueFromDB(guildID)
	if err != nil {
		return nil, err
	}

	if queue == nil {
		return nil, nil
	}

	cacheMux.Lock()
	cache[guildID] = &queueCache{
		queue:     queue,
		timestamp: time.Now(),
	}
	cacheMux.Unlock()

	logger.Debugf("Loaded queue for guild %s: %d songs", guildID, len(queue.Songs))
	return queue, nil
}

type queueRow struct {
	textChannelID  string
	voiceChannelID string
	paused         bool
	playing        bool
	loading        bool
}

type guildSettingsRow struct {
	volume            float64
	repeat            int
	sponsorBlock      bool
	showStartedTrack  bool
	normalization     bool
	fadeIn            bool
	fadeOut           bool
	autoMix           bool
	fadeOnStop        bool
	fadeInDuration    float64
	fadeOutDuration   float64
	autoMixBeats      int
	crossfade         bool
	crossfadeDuration float64
	trimSilence       bool
	styleVolume       string
	styleEQ           string
	styleFilter       string
	styleEffect       string
	styleLoop         string
}

func loadQueueRow(guildID string) (*queueRow, error) {
	var textChannelID, voiceChannelID string
	var paused, playing, loading int

	err := database.DB.QueryRow(
		`SELECT text_channel_id, voice_channel_id, paused,
		 COALESCE(playing, 0), COALESCE(loading, 0)
		 FROM queues WHERE guild_id = ?`,
		guildID,
	).Scan(&textChannelID, &voiceChannelID, &paused, &playing, &loading)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query queue: %w", err)
	}

	return &queueRow{
		textChannelID:  textChannelID,
		voiceChannelID: voiceChannelID,
		paused:         paused == 1,
		playing:        playing == 1,
		loading:        loading == 1,
	}, nil
}

func defaultGuildSettingsRow() guildSettingsRow {
	volume := 100.0
	if cfg := config.GetConfig(); cfg != nil {
		volume = cfg.DefaultVolume
	}

	return guildSettingsRow{
		volume:            volume,
		showStartedTrack:  true,
		fadeInDuration:    3,
		fadeOutDuration:   3,
		autoMixBeats:      16,
		crossfadeDuration: 8,
		styleVolume:       AutoMixStyleAuto,
		styleEQ:           AutoMixStyleAuto,
		styleFilter:       AutoMixStyleAuto,
		styleEffect:       AutoMixStyleAuto,
		styleLoop:         AutoMixStyleAuto,
	}
}

func loadGuildSettingsRow(guildID string) (guildSettingsRow, error) {
	var volume float64
	var repeat, sponsorblock, showStartedTrack, normalization int
	var fadein, fadeout, automix, fadeOnStop, automixBeats, crossfade, trimSilence int
	var fadeinDuration, fadeoutDuration, crossfadeDuration float64
	var styleVolume, styleEQ, styleFilter, styleEffect, styleLoop string

	err := database.DB.QueryRow(
		`SELECT volume, repeat, sponsorblock, show_started_track, normalization,
		 COALESCE(fadein, 0), COALESCE(fadeout, 0), COALESCE(automix, 0),
		 COALESCE(fade_on_stop, 0), COALESCE(fadein_duration, 3),
		 COALESCE(fadeout_duration, 3), COALESCE(automix_beats, 16),
		 COALESCE(crossfade, 0), COALESCE(crossfade_duration, 8),
		 COALESCE(trim_silence, 0), COALESCE(automix_style_volume, 'auto'),
		 COALESCE(automix_style_eq, 'auto'), COALESCE(automix_style_filter, 'auto'),
		 COALESCE(automix_style_effect, 'auto'), COALESCE(automix_style_loop, 'auto')
		 FROM guild_settings WHERE guild_id = ?`,
		guildID,
	).Scan(&volume, &repeat, &sponsorblock, &showStartedTrack, &normalization,
		&fadein, &fadeout, &automix, &fadeOnStop, &fadeinDuration,
		&fadeoutDuration, &automixBeats, &crossfade, &crossfadeDuration,
		&trimSilence, &styleVolume, &styleEQ, &styleFilter, &styleEffect, &styleLoop)

	if err == sql.ErrNoRows {
		settings := defaultGuildSettingsRow()
		logger.Debugf("No guild_settings found for guild %s, using defaults (volume=%g)", guildID, settings.volume)
		return settings, nil
	}
	if err != nil {
		return guildSettingsRow{}, fmt.Errorf("failed to query guild settings: %w", err)
	}

	logger.Debugf("Loaded guild_settings for guild %s: volume=%g, repeat=%t, sponsorblock=%t, normalization=%t",
		guildID, volume, repeat == 1, sponsorblock == 1, normalization == 1)

	return guildSettingsRow{
		volume:            volume,
		repeat:            repeat,
		sponsorBlock:      sponsorblock == 1,
		showStartedTrack:  showStartedTrack == 1,
		normalization:     normalization == 1,
		fadeIn:            fadein == 1,
		fadeOut:           fadeout == 1,
		autoMix:           automix == 1,
		fadeOnStop:        fadeOnStop == 1,
		fadeInDuration:    fadeinDuration,
		fadeOutDuration:   fadeoutDuration,
		autoMixBeats:      automixBeats,
		crossfade:         crossfade == 1,
		crossfadeDuration: crossfadeDuration,
		trimSilence:       trimSilence == 1,
		styleVolume:       styleVolume,
		styleEQ:           styleEQ,
		styleFilter:       styleFilter,
		styleEffect:       styleEffect,
		styleLoop:         styleLoop,
	}, nil
}

func loadQueueSongs(guildID string) ([]*Song, error) {
	rows, err := database.DB.Query(
		`SELECT id, guild_id, url, title, duration, thumbnail, requested_by_id,
		 requested_by_tag, queue_position, seek_time, uploader, is_live,
		 COALESCE(automix_style_volume, 'auto'), COALESCE(automix_style_eq, 'auto'),
		 COALESCE(automix_style_filter, 'auto'), COALESCE(automix_style_effect, 'auto'),
		 COALESCE(automix_style_loop, 'auto')
		 FROM songs WHERE guild_id = ? ORDER BY queue_position ASC`,
		guildID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query songs: %w", err)
	}
	defer rows.Close()

	songs := []*Song{}
	for rows.Next() {
		var song Song
		var isLive int
		err := rows.Scan(
			&song.ID, &song.GuildID, &song.URL, &song.Title, &song.Duration,
			&song.Thumbnail, &song.RequestedByID, &song.RequestedByTag,
			&song.QueuePosition, &song.SeekTime, &song.Uploader, &isLive,
			&song.AutoMixStyleVolume, &song.AutoMixStyleEQ, &song.AutoMixStyleFilter,
			&song.AutoMixStyleEffect, &song.AutoMixStyleLoop,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan song: %w", err)
		}
		song.IsLive = isLive == 1
		songs = append(songs, &song)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read songs: %w", err)
	}

	return songs, nil
}

func loadQueueFromDB(guildID string) (*Queue, error) {
	row, err := loadQueueRow(guildID)
	if err != nil || row == nil {
		return nil, err
	}

	settings, err := loadGuildSettingsRow(guildID)
	if err != nil {
		return nil, err
	}

	songs, err := loadQueueSongs(guildID)
	if err != nil {
		return nil, err
	}

	return &Queue{
		GuildID:            guildID,
		TextChannelID:      row.textChannelID,
		VoiceChannelID:     row.voiceChannelID,
		Songs:              songs,
		Volume:             settings.volume,
		RepeatMode:         settings.repeat,
		SponsorBlock:       settings.sponsorBlock,
		ShowStartedTrack:   settings.showStartedTrack,
		Normalization:      settings.normalization,
		Paused:             row.paused,
		Playing:            row.playing,
		Loading:            row.loading,
		FadeIn:             settings.fadeIn,
		FadeOut:            settings.fadeOut,
		AutoMix:            settings.autoMix,
		FadeOnStop:         settings.fadeOnStop,
		FadeInDuration:     settings.fadeInDuration,
		FadeOutDuration:    settings.fadeOutDuration,
		AutoMixBeats:       settings.autoMixBeats,
		Crossfade:          settings.crossfade,
		CrossfadeDuration:  settings.crossfadeDuration,
		TrimSilence:        settings.trimSilence,
		AutoMixStyleVolume: settings.styleVolume,
		AutoMixStyleEQ:     settings.styleEQ,
		AutoMixStyleFilter: settings.styleFilter,
		AutoMixStyleEffect: settings.styleEffect,
		AutoMixStyleLoop:   settings.styleLoop,
	}, nil
}
