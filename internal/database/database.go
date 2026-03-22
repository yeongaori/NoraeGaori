package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"noraegaori/pkg/logger"
)

var (
	DB *sql.DB
)

// Initialize initializes the SQLite database and creates tables if needed
func Initialize() error {
	// Ensure data directory exists
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "database.sqlite")
	logger.Info(fmt.Sprintf("Opening database at: %s", dbPath))

	var err error
	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(5)

	// Test connection
	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("Database connection established")

	// Create tables
	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	logger.Info("Database tables initialized successfully")
	return nil
}

// createTables creates all necessary database tables with migrations
func createTables() error {
	// Guild settings table
	guildSettingsSQL := `
	CREATE TABLE IF NOT EXISTS guild_settings (
		guild_id TEXT PRIMARY KEY,
		volume REAL DEFAULT 100,
		repeat INTEGER DEFAULT 0,
		sponsorblock INTEGER DEFAULT 0,
		show_started_track INTEGER DEFAULT 1,
		normalization INTEGER DEFAULT 0
	);`

	// Queues table
	queuesSQL := `
	CREATE TABLE IF NOT EXISTS queues (
		guild_id TEXT PRIMARY KEY,
		text_channel_id TEXT NOT NULL,
		voice_channel_id TEXT NOT NULL,
		paused INTEGER DEFAULT 0
	);`

	// Songs table
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

	// Create index for faster queue position queries
	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_songs_guild_position
	ON songs(guild_id, queue_position);`

	// Execute table creation
	statements := []string{guildSettingsSQL, queuesSQL, songsSQL, indexSQL}
	for _, stmt := range statements {
		if _, err := DB.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute SQL statement: %w", err)
		}
	}

	// Run migrations
	if err := runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// runMigrations checks and adds new columns if they don't exist
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
	}

	for _, m := range migrations {
		// Check if column exists
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

		// Add column if it doesn't exist
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


// Close closes the database connection
func Close() error {
	if DB != nil {
		logger.Info("Closing database connection")
		return DB.Close()
	}
	return nil
}
