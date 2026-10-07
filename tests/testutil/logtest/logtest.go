package logtest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"noraegaori/internal/logger"
)

func Capture(t *testing.T) func() string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.log")
	logger.SetLogFile(path)
	t.Cleanup(func() { logger.SetLogFile("") })

	return func() string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read the captured log: %v", err)
		}
		return string(content)
	}
}

func RunWithDebug(m *testing.M) {
	*logger.HookDebugMode = true
	os.Exit(m.Run())
}

func CaptureConsole(t *testing.T) func() string {
	t.Helper()

	buffer := &bytes.Buffer{}

	logger.HookOutMu.Lock()
	previousOutput := *logger.HookOutput
	*logger.HookOutput = buffer
	logger.HookOutMu.Unlock()

	t.Cleanup(func() {
		logger.HookOutMu.Lock()
		*logger.HookOutput = previousOutput
		logger.HookOutMu.Unlock()
	})

	return func() string {
		logger.HookOutMu.Lock()
		defer logger.HookOutMu.Unlock()
		return buffer.String()
	}
}
