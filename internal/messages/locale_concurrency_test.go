package messages

import (
	"sync"
	"testing"
)

func TestLoadLocaleRacesWithReaders(t *testing.T) {
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if T().Titles.Error == "" && Lang() == "" {
					t.Error("the active locale was observed in an unset state")
					return
				}
			}
		}()
	}

	for i := 0; i < 40; i++ {
		if err := LoadLocale("en"); err != nil {
			t.Errorf("LoadLocale returned %v, want nil", err)
			break
		}
	}

	close(stop)
	wg.Wait()
}

func TestActiveLocaleAndLangStayConsistent(t *testing.T) {
	if err := LoadLocale("en"); err != nil {
		t.Fatalf("LoadLocale returned %v, want nil", err)
	}

	active := activeLocale.Load()
	if active == nil {
		t.Fatal("no active locale is stored")
	}
	if active.lang != "en" {
		t.Errorf("got lang %q, want %q", active.lang, "en")
	}
	if active.locale == nil {
		t.Fatal("the active locale pointer is nil")
	}
	if active.locale.Titles.Error == "" {
		t.Error("the active locale carries no strings")
	}

	if got := Lang(); got != active.lang {
		t.Errorf("Lang() = %q but the stored pair says %q", got, active.lang)
	}
	if got := T(); got != active.locale {
		t.Error("T() returned a locale that is not the stored one")
	}
}

func TestLoadLocaleFallsBackToEmbeddedEnglish(t *testing.T) {
	err := LoadLocale("definitely-not-a-locale")
	if err == nil {
		t.Fatal("LoadLocale returned nil, want an error for an unknown locale")
	}

	if T().Titles.Error == "" {
		t.Error("the fallback locale carries no strings")
	}

	if loadErr := LoadLocale("en"); loadErr != nil {
		t.Fatalf("failed to restore the English locale: %v", loadErr)
	}
}
