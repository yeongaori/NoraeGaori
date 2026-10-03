package localesync_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"noraegaori/internal/localesync"
	"noraegaori/locales"
)

type memoryStore struct {
	baselines map[string]string
	loadErr   error
	saveErr   error
}

func (store *memoryStore) Load(lang string) ([]byte, bool, error) {
	if store.loadErr != nil {
		return nil, false, store.loadErr
	}
	content, found := store.baselines[lang]
	return []byte(content), found, nil
}

func (store *memoryStore) Save(lang string, content []byte) error {
	if store.saveErr != nil {
		return store.saveErr
	}
	store.baselines[lang] = string(content)
	return nil
}

func storeWith(baselines map[string]string) *memoryStore {
	return &memoryStore{baselines: baselines}
}

func shippedLocales(files map[string]string) fstest.MapFS {
	shipped := fstest.MapFS{}
	for name, content := range files {
		shipped[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return shipped
}

func writeUserLocale(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
	return path
}

func readUserLocale(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var locale map[string]any
	if err := json.Unmarshal(data, &locale); err != nil {
		t.Fatalf("%s is not valid JSON after the sync: %v\n%s", path, err, data)
	}
	return locale
}

func syncOne(t *testing.T, shipped, baseline, user string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	path := writeUserLocale(t, dir, "ko.json", user)
	baselines := map[string]string{}
	if baseline != "" {
		baselines["ko"] = baseline
	}
	if err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": shipped}), storeWith(baselines)); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	return readUserLocale(t, path)
}

func firstDifference(got, want string) string {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			return fmt.Sprintf("line %d is %q, want %q", i+1, gotLines[i], wantLines[i])
		}
	}
	return fmt.Sprintf("got %d lines, want %d", len(gotLines), len(wantLines))
}

func section(t *testing.T, locale map[string]any, name string) map[string]any {
	t.Helper()
	values, ok := locale[name].(map[string]any)
	if !ok {
		t.Fatalf("section %q is missing or not an object: %v", name, locale[name])
	}
	return values
}

func TestSyncCreatesMissingLocaleFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	shipped := `{"player": {"now_playing": "Now playing"}}`
	store := storeWith(map[string]string{})

	if err := localesync.Sync(dir, shippedLocales(map[string]string{"en.json": shipped}), store); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "en.json"))
	if err != nil {
		t.Fatalf("the missing locale was not created: %v", err)
	}
	if string(data) != shipped {
		t.Errorf("created file = %q, want the shipped bytes %q", data, shipped)
	}
	if store.baselines["en"] != shipped {
		t.Errorf("stored baseline = %q, want the shipped locale", store.baselines["en"])
	}
}

func TestSyncUpdatesStringsTheUserNeverTouched(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing ▶"}}`,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Now playing"}}`,
	)

	if got := section(t, locale, "player")["now_playing"]; got != "Now playing ▶" {
		t.Errorf("untouched string = %q, want the new shipped wording", got)
	}
}

func TestSyncKeepsEditsAcrossSeveralUpgrades(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ko.json")
	store := storeWith(map[string]string{})
	upgrade := func(shipped string) {
		t.Helper()
		if err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": shipped}), store); err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
	}

	upgrade(`{"player": {"now_playing": "Now playing", "paused": "Paused"}}`)
	writeUserLocale(t, dir, "ko.json", `{"player": {"now_playing": "Playing!", "paused": "Paused"}}`)
	upgrade(`{"player": {"now_playing": "Now playing ▶", "paused": "Paused"}}`)
	upgrade(`{"player": {"now_playing": "Now playing ▶▶", "paused": "Paused ⏸"}}`)

	player := section(t, readUserLocale(t, path), "player")
	if got := player["now_playing"]; got != "Playing!" {
		t.Errorf("after two upgrades the user's edit = %q, want it still kept", got)
	}
	if got := player["paused"]; got != "Paused ⏸" {
		t.Errorf("after two upgrades an untouched string = %q, want the latest wording", got)
	}
}

func TestSyncKeepsTheUsersStringsWhenALanguageStartsShipping(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "지금 재생 중", "paused": "일시정지됨"}}`,
		"",
		`{"player": {"now_playing": "재생 중"}}`,
	)

	player := section(t, locale, "player")
	if got := player["now_playing"]; got != "재생 중" {
		t.Errorf("the user's own translation = %q, want it kept", got)
	}
	if got := player["paused"]; got != "일시정지됨" {
		t.Errorf("a string the user's translation lacked = %v, want the shipped one added", got)
	}
}

func TestSyncTreatsDifferentlyEscapedTextAsUntouched(t *testing.T) {
	escaped := strconv.QuoteToASCII("지금 재생 중")
	if !strings.Contains(escaped, `\`) {
		t.Fatalf("%s has no escapes, so the test would compare identical bytes", escaped)
	}
	locale := syncOne(t,
		`{"player": {"now_playing": "지금 재생 중 ▶"}}`,
		`{"player": {"now_playing": "지금 재생 중"}}`,
		`{"player": {"now_playing": `+escaped+`}}`,
	)

	if got := section(t, locale, "player")["now_playing"]; got != "지금 재생 중 ▶" {
		t.Errorf("a string saved with escaped characters = %q, want it updated as untouched", got)
	}
}

func TestSyncKeepsStringsTheUserEdited(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing ▶"}}`,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Playing right now"}}`,
	)

	if got := section(t, locale, "player")["now_playing"]; got != "Playing right now" {
		t.Errorf("edited string = %q, want the user's edit", got)
	}
}

func TestSyncAddsNewStrings(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing", "queue_full": "The queue is full"}, "votes": {"start": "Vote started"}}`,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Playing right now"}}`,
	)

	player := section(t, locale, "player")
	if got := player["queue_full"]; got != "The queue is full" {
		t.Errorf("new string = %v, want it added", got)
	}
	if got := player["now_playing"]; got != "Playing right now" {
		t.Errorf("edited string = %q, want the user's edit next to the new one", got)
	}
	if got := section(t, locale, "votes")["start"]; got != "Vote started" {
		t.Errorf("new section = %v, want it added", locale["votes"])
	}
}

func TestSyncLeavesStringsTheUserDeletedDeleted(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing", "paused": "Paused"}}`,
		`{"player": {"now_playing": "Now playing", "paused": "Paused"}}`,
		`{"player": {"now_playing": "Now playing"}}`,
	)

	if got, found := section(t, locale, "player")["paused"]; found {
		t.Errorf("a string the user deleted came back as %q", got)
	}
}

func TestSyncDropsUntouchedStringsRemovedUpstream(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Now playing", "legacy": "Old text"}}`,
		`{"player": {"now_playing": "Now playing", "legacy": "Old text"}}`,
	)

	if got, found := section(t, locale, "player")["legacy"]; found {
		t.Errorf("an untouched string removed upstream was kept as %q", got)
	}
}

func TestSyncKeepsEditedStringsRemovedUpstream(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Now playing", "legacy": "Old text"}}`,
		`{"player": {"now_playing": "Now playing", "legacy": "My text"}}`,
	)

	if got := section(t, locale, "player")["legacy"]; got != "My text" {
		t.Errorf("an edited string removed upstream = %v, want the user's edit kept", got)
	}
}

func TestSyncKeepsStringsTheUserAdded(t *testing.T) {
	dir := t.TempDir()
	path := writeUserLocale(t, dir, "ko.json", "{\n  \"player\": {\n    \"mine\": \"Mine\",\n    \"now_playing\": \"Now playing\"\n  }\n}\n")
	shipped := shippedLocales(map[string]string{"ko.json": "{\n  \"player\": {\n    \"now_playing\": \"Now playing\",\n    \"paused\": \"Paused\"\n  }\n}\n"})
	store := storeWith(map[string]string{"ko": `{"player": {"now_playing": "Now playing"}}`})

	if err := localesync.Sync(dir, shipped, store); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	want := "{\n  \"player\": {\n    \"now_playing\": \"Now playing\",\n    \"paused\": \"Paused\",\n    \"mine\": \"Mine\"\n  }\n}\n"
	if string(data) != want {
		t.Errorf("file =\n%s\nwant the shipped keys in shipped order, then the user's own key:\n%s", data, want)
	}
}

func TestSyncKeepsTheUserValueWhenTheTypeDiffers(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": "Disabled"}`,
	)

	if got := locale["player"]; got != "Disabled" {
		t.Errorf("player = %v, want the user's value kept when its type differs", got)
	}
}

func TestSyncTreatsAliasListsAsOneValue(t *testing.T) {
	locale := syncOne(t,
		`{"commands": {"play": {"aliases": ["play", "p", "pl"]}, "skip": {"aliases": ["skip", "s", "sk"]}}}`,
		`{"commands": {"play": {"aliases": ["play", "p"]}, "skip": {"aliases": ["skip", "s"]}}}`,
		`{"commands": {"play": {"aliases": ["play", "p", "재생"]}, "skip": {"aliases": ["skip", "s"]}}}`,
	)

	commands := section(t, locale, "commands")
	play := commands["play"].(map[string]any)["aliases"].([]any)
	if len(play) != 3 || play[2] != "재생" {
		t.Errorf("edited aliases = %v, want the user's list kept whole", play)
	}
	skip := commands["skip"].(map[string]any)["aliases"].([]any)
	if len(skip) != 3 || skip[2] != "sk" {
		t.Errorf("untouched aliases = %v, want the new shipped list", skip)
	}
}

func TestSyncWithoutABaselineKeepsDifferencesAndAddsMissingStrings(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing ▶", "paused": "Paused", "stopped": "Stopped"}}`,
		"",
		`{"player": {"now_playing": "Playing right now", "paused": "Paused"}}`,
	)

	player := section(t, locale, "player")
	if got := player["now_playing"]; got != "Playing right now" {
		t.Errorf("a string differing from the shipped one = %q, want it kept as the user's edit", got)
	}
	if got := player["stopped"]; got != "Stopped" {
		t.Errorf("a missing string = %v, want it added", got)
	}
}

func TestSyncLeavesAnInvalidUserFileAlone(t *testing.T) {
	dir := t.TempDir()
	broken := `{"player": {"now_playing": "Now playing",}}`
	path := writeUserLocale(t, dir, "ko.json", broken)
	store := storeWith(map[string]string{"ko": `{"player": {"now_playing": "Now playing"}}`})

	err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": `{"player": {"now_playing": "Now playing ▶"}}`}), store)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("Sync error = %v, want one naming %s", err, path)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("failed to read %s: %v", path, readErr)
	}
	if string(data) != broken {
		t.Errorf("an invalid file was rewritten to %q", data)
	}
	if got := store.baselines["ko"]; got != `{"player": {"now_playing": "Now playing"}}` {
		t.Errorf("baseline = %q, want it unchanged so the next sync still sees the old text", got)
	}
}

func TestSyncDoesNotRewriteAFileThatNeedsNoChange(t *testing.T) {
	dir := t.TempDir()
	user := "{\"player\":{\"paused\":\"Paused\",\"now_playing\":\"Now playing\"}}"
	path := writeUserLocale(t, dir, "ko.json", user)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("failed to backdate %s: %v", path, err)
	}
	shipped := `{"player": {"now_playing": "Now playing", "paused": "Paused"}}`

	if err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": shipped}), storeWith(map[string]string{"ko": shipped})); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	if string(data) != user || !info.ModTime().Equal(past) {
		t.Errorf("a file needing no change was rewritten: %q at %v", data, info.ModTime())
	}
}

func TestSyncFormatsLikeTheShippedFiles(t *testing.T) {
	dir := t.TempDir()
	path := writeUserLocale(t, dir, "ko.json", `{"a": {"text": "<b>Tom & Jerry</b>"}}`)
	shipped := "{\n" +
		"  \"b\": {\n" +
		"    \"z\": \"Z\"\n" +
		"  },\n" +
		"\n" +
		"  \"a\": {\n" +
		"    \"text\": \"<b>Tom & Jerry</b>\",\n" +
		"    \"options\": { \"query\": \"Q\", \"page\": \"P\" },\n" +
		"    \"aliases\": [\"x\", \"y\"],\n" +
		"    \"long\": [\n" +
		"      \"first\",\n" +
		"      \"second\"\n" +
		"    ],\n" +
		"    \"empty\": [],\n" +
		"    \"nothing\": {}\n" +
		"  }\n" +
		"}\n"

	if err := localesync.Sync(dir, shippedLocales(map[string]string{"ko.json": shipped}), storeWith(map[string]string{})); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	if string(data) != shipped {
		t.Errorf("file =\n%s\nwant the shipped layout (key order, blank line, inline and multi-line values, unescaped HTML characters):\n%s", data, shipped)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temporary file was left behind: %v", err)
	}
}

func TestSyncReproducesTheShippedLocalesByteForByte(t *testing.T) {
	for _, name := range []string{"en.json", "ko.json"} {
		t.Run(name, func(t *testing.T) {
			shipped, err := locales.Files.ReadFile(name)
			if err != nil {
				t.Fatalf("failed to read the embedded %s: %v", name, err)
			}
			lines := strings.Split(string(shipped), "\n")
			var user []string
			removed := false
			for _, line := range lines {
				if !removed && strings.HasPrefix(line, `    "empty_queue": `) && strings.HasSuffix(line, ",") {
					removed = true
					continue
				}
				user = append(user, line)
			}
			if !removed {
				t.Fatalf("%s has no errors.empty_queue line to remove", name)
			}

			dir := t.TempDir()
			path := writeUserLocale(t, dir, name, strings.Join(user, "\n"))
			if err := localesync.Sync(dir, shippedLocales(map[string]string{name: string(shipped)}), storeWith(map[string]string{})); err != nil {
				t.Fatalf("Sync failed: %v", err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read %s: %v", path, err)
			}
			if string(data) != string(shipped) {
				t.Errorf("re-adding one string did not reproduce the shipped %s byte for byte: %s", name, firstDifference(string(data), string(shipped)))
			}
		})
	}
}

func TestSyncContinuesPastAFailingLocale(t *testing.T) {
	for _, test := range []struct{ broken, healthy string }{
		{broken: "en.json", healthy: "ko.json"},
		{broken: "ko.json", healthy: "en.json"},
	} {
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

func TestSyncLeavesLanguagesTheUserAddedAlone(t *testing.T) {
	dir := t.TempDir()
	added := "{\"player\":{\"now_playing\":\"再生中\"}}"
	path := writeUserLocale(t, dir, "ja.json", added)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("failed to backdate %s: %v", path, err)
	}
	store := storeWith(map[string]string{})

	if err := localesync.Sync(dir, shippedLocales(map[string]string{"en.json": `{"player": {"now_playing": "Now playing", "paused": "Paused"}}`}), store); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	if string(data) != added || !info.ModTime().Equal(past) {
		t.Errorf("a language the user added was changed: %q at %v", data, info.ModTime())
	}
	if _, found := store.baselines["ja"]; found {
		t.Error("a baseline was stored for a language the bot does not ship")
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
	for _, test := range []struct {
		name string
		user string
	}{
		{name: "creating a missing file"},
		{name: "updating an existing file", user: `{"player": {}}`},
	} {
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

func TestSyncKeepsASectionRemovedUpstreamThatTheUserChanged(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Now playing"}, "legacy": {"one": "One"}}`,
		`{"player": {"now_playing": "Now playing"}, "legacy": {"one": "One", "two": "Two"}}`,
	)

	if got := section(t, locale, "legacy")["two"]; got != "Two" {
		t.Errorf("a removed section the user extended = %v, want it kept", locale["legacy"])
	}
}

func TestSyncKeepsARemovedStringTheUserTurnedIntoASection(t *testing.T) {
	locale := syncOne(t,
		`{"player": {"now_playing": "Now playing"}}`,
		`{"player": {"now_playing": "Now playing"}, "legacy": "Old text"}`,
		`{"player": {"now_playing": "Now playing"}, "legacy": {"one": "One"}}`,
	)

	if got := section(t, locale, "legacy")["one"]; got != "One" {
		t.Errorf("legacy = %v, want the user's section kept", locale["legacy"])
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
