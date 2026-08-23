package database

import (
	"database/sql"
	"fmt"
	"noraegaori/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTestDB(t *testing.T, db *sql.DB) {
	t.Helper()

	testutil.Swap(t, &DB, db)
}

func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func createLegacySchema(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE guild_settings (guild_id TEXT PRIMARY KEY)`,
		`CREATE TABLE queues (guild_id TEXT PRIMARY KEY, text_channel_id TEXT NOT NULL, voice_channel_id TEXT NOT NULL)`,
		`CREATE TABLE songs (id INTEGER PRIMARY KEY AUTOINCREMENT, guild_id TEXT NOT NULL, url TEXT NOT NULL, title TEXT NOT NULL, queue_position INTEGER NOT NULL)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("failed to create legacy schema: %v", err)
		}
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatalf("failed to read columns of %s: %v", table, err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, dfltValue, pk interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("failed to scan column info: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate columns of %s: %v", table, err)
	}

	return columns
}

var migratedColumns = map[string][]string{
	"guild_settings": {
		"show_started_track", "normalization", "language", "prefix",
		"fadein", "fadeout", "automix", "fade_on_stop",
		"fadein_duration", "fadeout_duration", "automix_beats",
		"crossfade", "crossfade_duration", "trim_silence",
		"automix_style_volume", "automix_style_eq", "automix_style_filter",
		"automix_style_effect", "automix_style_loop",
	},
	"queues": {"paused", "playing", "loading"},
	"songs": {
		"seek_time", "uploader", "is_live",
		"automix_style_volume", "automix_style_eq", "automix_style_filter",
		"automix_style_effect", "automix_style_loop",
	},
}

func assertMigratedColumns(t *testing.T, db *sql.DB) {
	t.Helper()

	for table, wanted := range migratedColumns {
		columns := tableColumns(t, db, table)
		for _, column := range wanted {
			if !columns[column] {
				t.Errorf("%s.%s is missing after migrations", table, column)
			}
		}
	}
}

func TestInitializeCreatesSchema(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		Close()
		DB = nil
	})

	if err := Initialize(); err != nil {
		t.Fatalf("Initialize returned %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join("data", "database.sqlite")); err != nil {
		t.Errorf("database file was not created: %v", err)
	}

	for _, table := range []string{"guild_settings", "queues", "songs", "track_analysis"} {
		if len(tableColumns(t, DB, table)) == 0 {
			t.Errorf("table %s was not created", table)
		}
	}

	for _, index := range []string{"idx_songs_guild_position", "idx_track_analysis_analyzed_at"} {
		var name string
		if err := DB.QueryRow("SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&name); err != nil {
			t.Errorf("index %s was not created: %v", index, err)
		}
	}

	assertMigratedColumns(t, DB)
}

func TestInitializeIsIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		Close()
		DB = nil
	})

	if err := Initialize(); err != nil {
		t.Fatalf("first Initialize returned %v, want nil", err)
	}
	first := len(tableColumns(t, DB, "guild_settings"))

	if err := Close(); err != nil {
		t.Fatalf("Close returned %v, want nil", err)
	}

	if err := Initialize(); err != nil {
		t.Fatalf("second Initialize returned %v, want nil", err)
	}
	second := len(tableColumns(t, DB, "guild_settings"))

	if first != second {
		t.Errorf("guild_settings has %d columns after re-initialization, want %d", second, first)
	}
}

func TestMigrationsAddMissingColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations returned %v, want nil", err)
	}

	assertMigratedColumns(t, db)
}

func TestMigrationsApplyColumnDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "defaults.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations returned %v, want nil", err)
	}

	if _, err := db.Exec("INSERT INTO guild_settings (guild_id) VALUES (?)", "guild"); err != nil {
		t.Fatalf("failed to insert guild settings: %v", err)
	}

	var style string
	var beats int
	if err := db.QueryRow("SELECT automix_style_volume, automix_beats FROM guild_settings WHERE guild_id = ?", "guild").Scan(&style, &beats); err != nil {
		t.Fatalf("failed to read migrated defaults: %v", err)
	}

	if style != "auto" {
		t.Errorf("automix_style_volume defaulted to %q, want \"auto\"", style)
	}
	if beats != 16 {
		t.Errorf("automix_beats defaulted to %d, want 16", beats)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "repeat.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)

	if err := runMigrations(); err != nil {
		t.Fatalf("first runMigrations returned %v, want nil", err)
	}
	first := len(tableColumns(t, db, "guild_settings"))

	if err := runMigrations(); err != nil {
		t.Fatalf("second runMigrations returned %v, want nil", err)
	}
	second := len(tableColumns(t, db, "guild_settings"))

	if first != second {
		t.Errorf("guild_settings has %d columns after a second run, want %d", second, first)
	}
}

func TestMigrationFailureIsReturned(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "readonly.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath+"?mode=ro")
	useTestDB(t, db)

	err := runMigrations()
	if err == nil {
		t.Fatal("runMigrations returned nil, want an error on a read-only database")
	}
	if !strings.Contains(err.Error(), "guild_settings.show_started_track") {
		t.Errorf("error %q does not name the column that failed", err)
	}

	columns := tableColumns(t, db, "guild_settings")
	if columns["show_started_track"] {
		t.Error("show_started_track exists, so the read-only database did not reject the migration")
	}
}

func TestCreateTablesFailsOnReadOnlyDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "readonly.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath+"?mode=ro")
	useTestDB(t, db)

	if err := createTables(); err == nil {
		t.Fatal("createTables returned nil, want an error on a read-only database")
	}
}

func TestInitializeFailsWhenDataDirectoryIsAFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { DB = nil })

	if err := os.WriteFile("data", []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to create the blocking file: %v", err)
	}

	if err := Initialize(); err == nil {
		t.Fatal("Initialize returned nil, want an error when data cannot be created")
	}
}

func TestCloseWithoutDatabase(t *testing.T) {
	useTestDB(t, nil)

	if err := Close(); err != nil {
		t.Errorf("Close returned %v, want nil", err)
	}
}
