package player

import (
	"sync"
	"testing"
	"time"
)

func TestAbortPlaybackIsIdempotent(t *testing.T) {
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	abortPlayback := func() { abortOnce.Do(func() { close(abortCh) }) }

	abortPlayback()
	abortPlayback()
	abortPlayback()

	select {
	case <-abortCh:
	case <-time.After(time.Second):
		t.Fatal("the abort channel was never closed")
	}
}

func TestAnnounceGoroutineStopsWhenAborted(t *testing.T) {
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	abortPlayback := func() { abortOnce.Do(func() { close(abortCh) }) }

	firstFrameCh := make(chan struct{})
	stopChan := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		select {
		case <-firstFrameCh:
		case <-abortCh:
		case <-stopChan:
		}
	}()

	abortPlayback()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the announce goroutine outlived the abort signal")
	}
}
