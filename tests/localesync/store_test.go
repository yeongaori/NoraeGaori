package localesync_test

import (
	"testing"

	"noraegaori/internal/localesync"
	"noraegaori/tests/testutil/dbtest"
)

func TestDatabaseStoreSavesAndReplacesBaselines(t *testing.T) {
	dbtest.Setup(t)
	store := localesync.DatabaseStore{}

	if _, found, err := store.Load("ko"); err != nil || found {
		t.Fatalf("Load before any save = found %v, err %v; want nothing stored", found, err)
	}

	for _, content := range []string{`{"a": "first"}`, `{"a": "second"}`} {
		if err := store.Save("ko", []byte(content)); err != nil {
			t.Fatalf("Save(%s) failed: %v", content, err)
		}
		got, found, err := store.Load("ko")
		if err != nil || !found || string(got) != content {
			t.Errorf("Load after Save(%s) = %q, found %v, err %v", content, got, found, err)
		}
	}

	if _, found, err := store.Load("en"); err != nil || found {
		t.Errorf("Load of another language = found %v, err %v; want nothing stored", found, err)
	}
}

func TestDatabaseStoreReportsDatabaseFailures(t *testing.T) {
	dbtest.Setup(t)
	dbtest.CloseUntilCleanup(t)
	store := localesync.DatabaseStore{}

	if _, found, err := store.Load("ko"); err == nil || found {
		t.Errorf("Load on a closed database = found %v, err %v; want an error", found, err)
	}
	if err := store.Save("ko", []byte(`{}`)); err == nil {
		t.Error("Save on a closed database succeeded")
	}
}
