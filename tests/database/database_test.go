package database_test

import (
	"database/sql"
	"fmt"
	"noraegaori/internal/database"
	"noraegaori/tests/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func useTestDB(t *testing.T, db *sql.DB) {
	t.Helper()

	testutil.Swap(t, &database.DB, db)
}

func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func createLegacySchema(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE guild_settings (guild_id TEXT PRIMARY KEY)`,
		`CREATE TABLE queues (guild_id TEXT PRIMARY KEY, text_channel_id TEXT NOT NULL, voice_channel_id TEXT NOT NULL)`,
		`CREATE TABLE songs (id INTEGER PRIMARY KEY AUTOINCREMENT, guild_id TEXT NOT NULL, url TEXT NOT NULL, title TEXT NOT NULL, queue_position INTEGER NOT NULL)`,
		`CREATE TABLE track_analysis (url TEXT NOT NULL, segment TEXT NOT NULL, bpm REAL, period_sec REAL, first_beat REAL, duration REAL, tonic INTEGER, minor INTEGER, key_confidence REAL, downbeat_phase INTEGER, analysis_version INTEGER NOT NULL, analyzed_at INTEGER NOT NULL, PRIMARY KEY (url, segment))`,
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
		"crossfade", "crossfade_duration", "trim_silence", "automix_overrides",
		"auto_leave", "auto_pause", "auto_resume",
	},
	"queues":         {"paused", "playing", "loading"},
	"songs":          {"seek_time", "uploader", "is_live", "automix_overrides"},
	"track_analysis": {"offset_sec", "beat_strength", "bar_offsets"},
}

func assertMigrated(t *testing.T, db *sql.DB) {
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

func TestInitializeEnablesWAL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		database.Close()
		database.DB = nil
	})

	if err := database.Initialize(); err != nil {
		t.Fatalf("Initialize returned %v, want nil", err)
	}

	var journalMode string
	if err := database.DB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("got journal_mode %q, want wal so readers do not block the writer", journalMode)
	}

	var busyTimeout int
	if err := database.DB.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("failed to read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("got busy_timeout %d, want 5000 so concurrent writes wait instead of failing", busyTimeout)
	}
}

func TestInitializeEnforcesForeignKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		database.Close()
		database.DB = nil
	})

	if err := database.Initialize(); err != nil {
		t.Fatalf("Initialize returned %v, want nil", err)
	}

	var foreignKeys int
	if err := database.DB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("failed to read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("got foreign_keys %d, want 1 so deleting a queue also deletes its songs", foreignKeys)
	}

	var synchronous int
	if err := database.DB.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("failed to read synchronous: %v", err)
	}
	if synchronous != 1 {
		t.Errorf("got synchronous %d, want 1 so WAL commits skip the extra fsync", synchronous)
	}

	_, err := database.DB.Exec(`INSERT INTO songs (guild_id, url, title, requested_by_id, requested_by_tag, queue_position)
		VALUES (?, ?, ?, ?, ?, 0)`, "queueless-guild", "queueless-guild", "queueless-guild", "queueless-guild", "queueless-guild")
	if err == nil {
		t.Error("a song without a queue was inserted, want a foreign key error")
	}
}

func TestInitializeLocksTransactionsEarly(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		database.Close()
		database.DB = nil
	})

	if err := database.Initialize(); err != nil {
		t.Fatalf("Initialize returned %v, want nil", err)
	}

	first, err := database.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	began := make(chan *sql.Tx, 1)
	go func() {
		second, err := database.DB.Begin()
		if err != nil {
			began <- nil
			return
		}
		began <- second
	}()

	select {
	case second := <-began:
		if second != nil {
			_ = second.Rollback()
		}
		_ = first.Rollback()
		t.Fatal("a second transaction began while the first was open, want it to wait for the write lock")
	case <-time.After(200 * time.Millisecond):
	}

	if err := first.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-began:
		if second == nil {
			t.Fatal("the second transaction never began after the first ended")
		}
		_ = second.Rollback()
	case <-time.After(2 * time.Second):
		t.Fatal("the second transaction never began after the first ended")
	}
}

func TestInitializeCreatesSchema(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		database.Close()
		database.DB = nil
	})

	if err := database.Initialize(); err != nil {
		t.Fatalf("Initialize returned %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join("data", "database.sqlite")); err != nil {
		t.Errorf("database file was not created: %v", err)
	}

	for _, table := range []string{"guild_settings", "queues", "songs", "track_analysis"} {
		if len(tableColumns(t, database.DB, table)) == 0 {
			t.Errorf("table %s was not created", table)
		}
	}

	for _, index := range []string{"idx_songs_guild_position", "idx_track_analysis_analyzed_at"} {
		var name string
		if err := database.DB.QueryRow("SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&name); err != nil {
			t.Errorf("index %s was not created: %v", index, err)
		}
	}

	assertMigrated(t, database.DB)
}

func TestInitializeIsIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		database.Close()
		database.DB = nil
	})

	if err := database.Initialize(); err != nil {
		t.Fatalf("first Initialize returned %v, want nil", err)
	}
	first := len(tableColumns(t, database.DB, "guild_settings"))

	if err := database.Close(); err != nil {
		t.Fatalf("Close returned %v, want nil", err)
	}

	if err := database.Initialize(); err != nil {
		t.Fatalf("second Initialize returned %v, want nil", err)
	}
	second := len(tableColumns(t, database.DB, "guild_settings"))

	if first != second {
		t.Errorf("guild_settings has %d columns after re-initialization, want %d", second, first)
	}
}

func TestRunMigrationsAddsColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)

	if err := database.HookRunMigrations(); err != nil {
		t.Fatalf("runMigrations returned %v, want nil", err)
	}

	assertMigrated(t, db)
}

func TestRunMigrationsDeletesSongsWithoutQueue(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)
	if _, err := db.Exec(`INSERT INTO queues (guild_id, text_channel_id, voice_channel_id) VALUES (?, ?, ?)`,
		"kept-guild", "kept-guild", "kept-guild"); err != nil {
		t.Fatalf("failed to seed queue state: %v", err)
	}
	for i, guildID := range []string{"kept-guild", "queueless-guild"} {
		if _, err := db.Exec(`INSERT INTO songs (guild_id, url, title, queue_position) VALUES (?, ?, ?, 0)`,
			guildID, guildID, guildID); err != nil {
			t.Fatalf("failed to seed song %d: %v", i, err)
		}
	}

	if err := database.HookRunMigrations(); err != nil {
		t.Fatalf("runMigrations returned %v, want nil", err)
	}

	for guildID, want := range map[string]int{"kept-guild": 1, "queueless-guild": 0} {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM songs WHERE guild_id = ?`, guildID).Scan(&got); err != nil {
			t.Fatalf("failed to count songs: %v", err)
		}
		if got != want {
			t.Errorf("got %d songs for %s, want %d", got, guildID, want)
		}
	}
}

func TestRunMigrationsDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "defaults.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)

	if err := database.HookRunMigrations(); err != nil {
		t.Fatalf("runMigrations returned %v, want nil", err)
	}

	if _, err := db.Exec("INSERT INTO guild_settings (guild_id) VALUES (?)", "guild"); err != nil {
		t.Fatalf("failed to insert guild settings: %v", err)
	}

	var overrides string
	var beats int
	if err := db.QueryRow("SELECT automix_overrides, automix_beats FROM guild_settings WHERE guild_id = ?", "guild").Scan(&overrides, &beats); err != nil {
		t.Fatalf("failed to read migrated defaults: %v", err)
	}

	if overrides != "" {
		t.Errorf("automix_overrides defaulted to %q, want none", overrides)
	}
	if beats != 64 {
		t.Errorf("automix_beats defaulted to %d, want 64", beats)
	}

	for _, column := range []string{"auto_leave", "auto_pause", "auto_resume"} {
		var enabled int
		if err := db.QueryRow("SELECT "+column+" FROM guild_settings WHERE guild_id = ?", "guild").Scan(&enabled); err != nil {
			t.Fatalf("failed to read %s: %v", column, err)
		}
		if enabled != 1 {
			t.Errorf("%s defaulted to %d, want 1", column, enabled)
		}
	}
}

func TestRunMigrationsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "repeat.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath)
	useTestDB(t, db)

	if err := database.HookRunMigrations(); err != nil {
		t.Fatalf("first runMigrations returned %v, want nil", err)
	}
	first := len(tableColumns(t, db, "guild_settings"))

	if err := database.HookRunMigrations(); err != nil {
		t.Fatalf("second runMigrations returned %v, want nil", err)
	}
	second := len(tableColumns(t, db, "guild_settings"))

	if first != second {
		t.Errorf("guild_settings has %d columns after a second run, want %d", second, first)
	}
}

func TestRunMigrationsFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "readonly.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath+"?mode=ro")
	useTestDB(t, db)

	err := database.HookRunMigrations()
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

func TestCreateTablesReadOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "readonly.sqlite")
	createLegacySchema(t, dbPath)

	db := openTestDB(t, "file:"+dbPath+"?mode=ro")
	useTestDB(t, db)

	if err := database.HookCreateTables(); err == nil {
		t.Fatal("createTables returned nil, want an error on a read-only database")
	}
}

func TestInitializeDataDirIsFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Cleanup(func() { database.DB = nil })

	if err := os.WriteFile("data", []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to create the blocking file: %v", err)
	}

	if err := database.Initialize(); err == nil {
		t.Fatal("Initialize returned nil, want an error when data cannot be created")
	}
}

func TestCloseNoDatabase(t *testing.T) {
	useTestDB(t, nil)

	if err := database.Close(); err != nil {
		t.Errorf("Close returned %v, want nil", err)
	}
}
