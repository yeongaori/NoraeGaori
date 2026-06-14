package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"noraegaori/pkg/logger"

	_ "github.com/mattn/go-sqlite3"
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
	logger.Debugf("[Database] Opening database at: %s", dbPath)

	var err error
	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(5)

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Debug("[Database] Database connection established")

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	logger.Debug("[Database] Database tables initialized successfully")
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

	statements := []string{guildSettingsSQL, queuesSQL, songsSQL, indexSQL}
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
		{"guild_settings", "automix_beats", "INTEGER DEFAULT 16"},
		{"guild_settings", "crossfade", "INTEGER DEFAULT 0"},
		{"guild_settings", "crossfade_duration", "REAL DEFAULT 8"},
		{"guild_settings", "trim_silence", "INTEGER DEFAULT 0"},
	}

	for _, m := range migrations {

		query := fmt.Sprintf("PRAGMA table_info(%s)", m.table)
		rows, err := DB.Query(query)
		if err != nil {
			return fmt.Errorf("failed to get table info for %s: %w", m.table, err)
		}

		columnExists := false
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull, dfltValue, pk interface{}
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
				rows.Close()
				return fmt.Errorf("failed to scan column info: %w", err)
			}
			if name == m.column {
				columnExists = true
				break
			}
		}
		rows.Close()

		if !columnExists {
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", m.table, m.column, m.typ)
			if _, err := DB.Exec(alterSQL); err != nil {
				logger.Warn(fmt.Sprintf("Failed to add column %s.%s (may already exist): %v", m.table, m.column, err))
			} else {
				logger.Info(fmt.Sprintf("Added column %s.%s", m.table, m.column))
			}
		}
	}

	return nil
}

func Close() error {
	if DB != nil {
		logger.Debug("Closing database connection")
		return DB.Close()
	}
	return nil
}
