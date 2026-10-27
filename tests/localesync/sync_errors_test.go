package localesync_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"noraegaori/internal/localesync"
)

func TestSyncContinuesPastAFailingLocale(t *testing.T) {
	tests := []struct{ broken, healthy string }{
		{broken: "en.json", healthy: "ko.json"},
		{broken: "ko.json", healthy: "en.json"},
	}
	for i := range tests {
		test := &tests[i]
		t.Run(test.broken, func(t *testing.T) {
			dir := t.TempDir()
			writeUserLocale(t, dir, test.broken, `not json`)
			shipped := shippedLocales(map[string]string{
				"en.json": `{"player": {"now_playing": "Now playing"}}`,
				"ko.json": `{"player": {"now_playing": "지금 재생 중"}}`,
			})

			err := localesync.Sync(dir, shipped, storeWith(map[string]string{}))
			if err == nil || !strings.Contains(err.Error(), test.broken) {
				t.Fatalf("Sync error = %v, want one naming %s", err, test.broken)
			}
			if _, statErr := os.Stat(filepath.Join(dir, test.healthy)); statErr != nil {
				t.Errorf("%s was not created after %s failed: %v", test.healthy, test.broken, statErr)
			}
		})
	}
}

func TestSyncReportsABaselineLoadFailureWithoutTouchingTheFile(t *testing.T) {
	dir := t.TempDir()
	user := `{"player": {"now_playing": "Now playing"}}`
	path := writeUserLocale(t, dir, "ko.json", user)
	store := &memoryStore{baselines: map[string]string{}, loadErr: errors.New("database is locked")}

	err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": `{"player": {"now_playing": "Now playing ▶"}}`}), store)
	if err == nil || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("Sync error = %v, want the store failure", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("failed to read %s: %v", path, readErr)
	}
	if string(data) != user {
		t.Errorf("the file was rewritten to %q although the old texts were unknown", data)
	}
}

func TestSyncTreatsAnUnreadableBaselineAsMissing(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing ▶", "paused": "Paused"}}`,
		`not json`,
		`{"player": {"now_playing": "Now playing"}}`,
	)

	player := section(t, locale, "player")
	if got := player["now_playing"]; got != "Now playing" {
		t.Errorf("with an unreadable baseline, a differing string = %q, want it kept as an edit", got)
	}
	if got := player["paused"]; got != "Paused" {
		t.Errorf("with an unreadable baseline, a missing string = %v, want it added", got)
	}
}

type unreadableLocales struct {
	fstest.MapFS
}

func (unreadableLocales) ReadFile(name string) ([]byte, error) {
	return nil, errors.New("embedded file is damaged")
}

func TestSyncReportsAnUnreadableShippedLocale(t *testing.T) {
	shipped := unreadableLocales{shippedLocales(map[string]string{"ko.json": `{"a": "A"}`})}

	err := localesync.Sync(t.TempDir(), shipped, storeWith(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "embedded file is damaged") {
		t.Fatalf("Sync error = %v, want the read failure", err)
	}
}

func TestSyncReportsAnInvalidShippedLocale(t *testing.T) {
	store := storeWith(map[string]string{})

	err := localesync.Sync(t.TempDir(), shippedLocales(map[string]string{"ko.json": `{"a": }`}), store)
	if err == nil || !strings.Contains(err.Error(), "ko.json") {
		t.Fatalf("Sync error = %v, want one naming the invalid shipped locale", err)
	}
	if len(store.baselines) != 0 {
		t.Errorf("an invalid shipped locale was stored as the baseline: %v", store.baselines)
	}
}

func TestSyncReportsADirectoryItCannotCreate(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0644); err != nil {
		t.Fatalf("failed to create %s: %v", blocker, err)
	}

	err := localesync.Sync(filepath.Join(blocker, "locales"), shippedLocales(map[string]string{"en.json": `{"a": "A"}`}), storeWith(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "failed to create") {
		t.Fatalf("Sync error = %v, want the directory creation failure", err)
	}
}

func TestSyncReportsAnUnreadableUserLocale(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "ko.json"), 0755); err != nil {
		t.Fatalf("failed to create the blocking directory: %v", err)
	}

	err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": `{"a": "A"}`}), storeWith(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("Sync error = %v, want the read failure", err)
	}
}

func TestSyncReportsAFailedWrite(t *testing.T) {
	tests := []struct {
		name string
		user string
	}{
		{name: "creating a missing file"},
		{name: "updating an existing file", user: `{"player": {}}`},
	}
	for i := range tests {
		test := &tests[i]
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.user != "" {
				writeUserLocale(t, dir, "ko.json", test.user)
			}
			if err := os.Mkdir(filepath.Join(dir, "ko.json.tmp"), 0755); err != nil {
				t.Fatalf("failed to create the blocking directory: %v", err)
			}
			store := storeWith(map[string]string{})

			err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": `{"player": {"now_playing": "Now playing"}}`}), store)
			if err == nil || !strings.Contains(err.Error(), "failed to write") {
				t.Fatalf("Sync error = %v, want the write failure", err)
			}
			if len(store.baselines) != 0 {
				t.Errorf("the baseline was saved although the file was not written: %v", store.baselines)
			}
		})
	}
}

func TestSyncReportsABaselineSaveFailure(t *testing.T) {
	dir := t.TempDir()
	store := &memoryStore{baselines: map[string]string{}, saveErr: errors.New("disk full")}

	err := localesync.Sync(dir, shippedLocales(map[string]string{"en.json": `{"a": "A"}`}), store)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Sync error = %v, want the save failure", err)
	}
}
