package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"noraegaori/internal/logger"

	_ "modernc.org/sqlite"
)

var (
	DB *sql.DB
)

func Initialize() error {

	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "database.sqlite")
	logger.Debugf("Opening database at: %s", dbPath)

	var err error
	DB, err = sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath))
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(5)

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Debug("Database connection established")

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	logger.Debug("Database tables initialized successfully")
	return nil
}

func createTables() error {

	guildSettingsSQL := `
	CREATE TABLE IF NOT EXISTS guild_settings (
		guild_id TEXT PRIMARY KEY,
		volume REAL DEFAULT 100,
		repeat INTEGER DEFAULT 0,
		sponsorblock INTEGER DEFAULT 0,
		show_started_track INTEGER DEFAULT 1,
		normalization INTEGER DEFAULT 0
	);`

	queuesSQL := `
	CREATE TABLE IF NOT EXISTS queues (
		guild_id TEXT PRIMARY KEY,
		text_channel_id TEXT NOT NULL,
		voice_channel_id TEXT NOT NULL,
		paused INTEGER DEFAULT 0
	);`

	songsSQL := `
	CREATE TABLE IF NOT EXISTS songs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT NOT NULL,
		url TEXT NOT NULL,
		title TEXT NOT NULL,
		duration TEXT,
		thumbnail TEXT,
		requested_by_id TEXT NOT NULL,
		requested_by_tag TEXT NOT NULL,
		queue_position INTEGER NOT NULL,
		seek_time INTEGER DEFAULT 0,
		uploader TEXT,
		is_live INTEGER DEFAULT 0,
		FOREIGN KEY (guild_id) REFERENCES queues(guild_id) ON DELETE CASCADE
	);`

	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_songs_guild_position
	ON songs(guild_id, queue_position);`

	trackAnalysisSQL := `
	CREATE TABLE IF NOT EXISTS track_analysis (
		url TEXT NOT NULL,
		segment TEXT NOT NULL,
		bpm REAL,
		period_sec REAL,
		first_beat REAL,
		duration REAL,
		offset_sec REAL DEFAULT 0,
		tonic INTEGER,
		minor INTEGER,
		key_confidence REAL,
		downbeat_phase INTEGER,
		beat_strength REAL DEFAULT 0,
		bar_offsets TEXT DEFAULT '',
		analysis_version INTEGER NOT NULL,
		analyzed_at INTEGER NOT NULL,
		PRIMARY KEY (url, segment)
	);`

	trackAnalysisIndexSQL := `
	CREATE INDEX IF NOT EXISTS idx_track_analysis_analyzed_at
	ON track_analysis(analyzed_at);`

	localeBaselinesSQL := `
	CREATE TABLE IF NOT EXISTS locale_baselines (
		lang TEXT PRIMARY KEY,
		content TEXT NOT NULL
	);`

	statements := []string{guildSettingsSQL, queuesSQL, songsSQL, indexSQL, trackAnalysisSQL, trackAnalysisIndexSQL, localeBaselinesSQL}
	for _, stmt := range statements {
		if _, err := DB.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute SQL statement: %w", err)
		}
	}

	if err := runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func runMigrations() error {
	migrations := []struct {
		table  string
		column string
		typ    string
	}{
		{"guild_settings", "show_started_track", "INTEGER DEFAULT 1"},
		{"guild_settings", "normalization", "INTEGER DEFAULT 0"},
		{"songs", "seek_time", "INTEGER DEFAULT 0"},
		{"songs", "uploader", "TEXT"},
		{"songs", "is_live", "INTEGER DEFAULT 0"},
		{"queues", "paused", "INTEGER DEFAULT 0"},
		{"queues", "playing", "INTEGER DEFAULT 0"},
		{"queues", "loading", "INTEGER DEFAULT 0"},
		{"guild_settings", "language", "TEXT"},
		{"guild_settings", "prefix", "TEXT"},
		{"guild_settings", "fadein", "INTEGER DEFAULT 0"},
		{"guild_settings", "fadeout", "INTEGER DEFAULT 0"},
		{"guild_settings", "automix", "INTEGER DEFAULT 0"},
		{"guild_settings", "fade_on_stop", "INTEGER DEFAULT 0"},
		{"guild_settings", "fadein_duration", "REAL DEFAULT 3"},
		{"guild_settings", "fadeout_duration", "REAL DEFAULT 3"},
		{"guild_settings", "automix_beats", "INTEGER DEFAULT 64"},
		{"guild_settings", "crossfade", "INTEGER DEFAULT 0"},
		{"guild_settings", "crossfade_duration", "REAL DEFAULT 8"},
		{"guild_settings", "trim_silence", "INTEGER DEFAULT 0"},
		{"guild_settings", "automix_overrides", "TEXT DEFAULT ''"},
		{"songs", "automix_overrides", "TEXT DEFAULT ''"},
		{"guild_settings", "auto_leave", "INTEGER DEFAULT 1"},
		{"guild_settings", "auto_pause", "INTEGER DEFAULT 1"},
		{"guild_settings", "auto_resume", "INTEGER DEFAULT 1"},
		{"track_analysis", "offset_sec", "REAL DEFAULT 0"},
		{"track_analysis", "beat_strength", "REAL DEFAULT 0"},
		{"track_analysis", "bar_offsets", "TEXT DEFAULT ''"},
	}

	for _, m := range migrations {
		columnExists, err := ColumnExists(m.table, m.column)
		if err != nil {
			return err
		}

		if !columnExists {
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", m.table, m.column, m.typ)
			if _, err := DB.Exec(alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s.%s: %w", m.table, m.column, err)
			}
			logger.Debugf("Added column %s.%s", m.table, m.column)
		}
	}

	return nil
}

func ColumnExists(table, column string) (bool, error) {
	rows, err := DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("failed to get table info for %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, dfltValue, pk interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return false, fmt.Errorf("failed to scan column info: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failed to read column info for %s: %w", table, err)
	}
	return false, nil
}

func Close() error {
	if DB != nil {
		logger.Debug("Closing database connection")
		return DB.Close()
	}
	return nil
}
