package recovery_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"noraegaori/internal/recovery"
	"noraegaori/tests/testutil/logtest"
)

func TestRunConvertsPanic(t *testing.T) {
	readLog := logtest.Capture(t)

	err := recovery.Run("probe", func() error {
		panic("probe exploded")
	})

	if err == nil || err.Error() != "probe panicked" {
		t.Fatalf("Run returned %v, want %q", err, "probe panicked")
	}
	if log := readLog(); !strings.Contains(log, "probe panicked: probe exploded") {
		t.Errorf("the log %q does not mention the panic", log)
	}
}

func TestRunReturnsError(t *testing.T) {
	want := errors.New("probe failed")

	if err := recovery.Run("probe", func() error {
		return want
	}); err != want {
		t.Errorf("Run returned %v, want %v", err, want)
	}
}

func TestGoRecoversPanic(t *testing.T) {
	readLog := logtest.Capture(t)

	recovery.Go("probe", func() {
		panic("probe exploded")
	})

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(readLog(), "probe panicked: probe exploded") {
		if time.Now().After(deadline) {
			t.Fatalf("the log %q does not mention the panic", readLog())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
