package messages_test

import (
	"sync"
	"testing"

	"noraegaori/internal/messages"
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
				if messages.T().Titles.Error == "" && messages.Lang() == "" {
					t.Error("the active locale was observed in an unset state")
					return
				}
			}
		}()
	}

	for i := 0; i < 40; i++ {
		if err := messages.LoadLocale("en"); err != nil {
			t.Errorf("LoadLocale returned %v, want nil", err)
			break
		}
	}

	close(stop)
	wg.Wait()
}

func TestActiveLocaleAndLangStayConsistent(t *testing.T) {
	if err := messages.LoadLocale("en"); err != nil {
		t.Fatalf("LoadLocale returned %v, want nil", err)
	}

	active := messages.HookActiveLocale.Load()
	if active == nil {
		t.Fatal("no active locale is stored")
	}
	if *active.HookLang() != "en" {
		t.Errorf("got lang %q, want %q", *active.HookLang(), "en")
	}
	if *active.HookLocale() == nil {
		t.Fatal("the active locale pointer is nil")
	}
	if (*active.HookLocale()).Titles.Error == "" {
		t.Error("the active locale carries no strings")
	}

	if got := messages.Lang(); got != *active.HookLang() {
		t.Errorf("Lang() = %q but the stored pair says %q", got, *active.HookLang())
	}
	if got := messages.T(); got != *active.HookLocale() {
		t.Error("T() returned a locale that is not the stored one")
	}
}

func TestLoadLocaleFallsBackToEmbeddedEnglish(t *testing.T) {
	err := messages.LoadLocale("definitely-not-a-locale")
	if err == nil {
		t.Fatal("LoadLocale returned nil, want an error for an unknown locale")
	}

	if messages.T().Titles.Error == "" {
		t.Error("the fallback locale carries no strings")
	}

	if loadErr := messages.LoadLocale("en"); loadErr != nil {
		t.Fatalf("failed to restore the English locale: %v", loadErr)
	}
}
