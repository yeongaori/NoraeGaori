package app

import (
	"testing"
	"time"
)

func TestForceExitAfterExitsWhenShutdownStalls(t *testing.T) {
	codes := make(chan int, 1)

	forceExitAfter(10*time.Millisecond, func(code int) { codes <- code })

	select {
	case code := <-codes:
		if code != 1 {
			t.Errorf("got exit code %d, want 1", code)
		}
	case <-time.After(time.Second):
		t.Fatal("the forced exit never fired")
	}
}

func TestForceExitAfterStaysQuietBeforeTheTimeout(t *testing.T) {
	codes := make(chan int, 1)

	forceExitAfter(time.Hour, func(code int) { codes <- code })

	select {
	case code := <-codes:
		t.Fatalf("the forced exit fired early with code %d", code)
	case <-time.After(50 * time.Millisecond):
	}
}
