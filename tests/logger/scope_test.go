package logger_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/logger"
	"noraegaori/tests/testutil"
)

func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()

	buffer := &bytes.Buffer{}

	logger.HookOutMu.Lock()
	previous := *logger.HookOutput
	*logger.HookOutput = buffer
	logger.HookOutMu.Unlock()

	t.Cleanup(func() {
		logger.HookOutMu.Lock()
		*logger.HookOutput = previous
		logger.HookOutMu.Unlock()
	})

	return buffer
}

func resetLogFileState(t *testing.T) {
	t.Helper()

	logger.HookOutMu.Lock()
	previousOutput := *logger.HookOutput
	previousFile := *logger.HookLogFile
	previousPath := *logger.HookLogFilePath
	previousBuf := *logger.HookEarlyBuf
	*logger.HookLogFile = nil
	*logger.HookLogFilePath = ""
	*logger.HookEarlyBuf = nil
	logger.HookOutMu.Unlock()

	t.Cleanup(func() {
		logger.HookOutMu.Lock()
		if *logger.HookLogFile != nil && *logger.HookLogFile != previousFile {
			(*logger.HookLogFile).Close()
		}
		*logger.HookOutput = previousOutput
		*logger.HookLogFile = previousFile
		*logger.HookLogFilePath = previousPath
		*logger.HookEarlyBuf = previousBuf
		logger.HookOutMu.Unlock()
	})
}

func TestDeriveTag(t *testing.T) {
	cases := map[string]string{
		"noraegaori/internal/youtube.(*AvailabilityPool).worker":        "worker",
		"noraegaori/internal/youtube.(*AvailabilityPool).start.gowrap1": "start",
		"noraegaori/internal/player.playAudio.func1":                    "playAudio",
		"noraegaori/internal/queue.Save-fm":                             "Save",
		"noraegaori/internal/rpc.UpdateRPC.deferwrap1":                  "UpdateRPC",
		"noraegaori/internal/database.runMigrations":                    "runMigrations",
		"main.main": "main",
		"":          "?",
	}

	for fullName, want := range cases {
		if got := logger.HookDeriveTag(fullName); got != want {
			t.Errorf("deriveTag(%q) = %q, want %q", fullName, got, want)
		}
	}
}

func TestDeriveTagFallsBackToPackageName(t *testing.T) {
	if got := logger.HookDeriveTag("noraegaori/internal/player.func1"); got != "player" {
		t.Errorf("got %q, want the package name when every part is generated", got)
	}
}

func TestIsDigits(t *testing.T) {
	cases := map[string]bool{
		"":    false,
		"1":   true,
		"42":  true,
		"12a": false,
		"a12": false,
		"-1":  false,
	}

	for input, want := range cases {
		if got := logger.HookIsDigits(input); got != want {
			t.Errorf("isDigits(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestIsGeneratedPart(t *testing.T) {
	cases := map[string]bool{
		"func1":      true,
		"gowrap1":    true,
		"deferwrap1": true,
		"3":          true,
		"func":       false,
		"gowrap":     false,
		"playAudio":  false,
		"funcName":   false,
	}

	for input, want := range cases {
		if got := logger.HookIsGeneratedPart(input); got != want {
			t.Errorf("isGeneratedPart(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestScopeUsesTheGivenTag(t *testing.T) {
	buffer := captureOutput(t)

	scope := logger.Scope("Worker 7")
	if scope.Tag() != "Worker 7" {
		t.Errorf("got tag %q, want %q", scope.Tag(), "Worker 7")
	}

	scope.Info("hello")
	if !strings.Contains(buffer.String(), "[Worker 7] hello") {
		t.Errorf("got %q, want it to carry the scope tag", buffer.String())
	}
}

func TestScopefFormatsTheTag(t *testing.T) {
	buffer := captureOutput(t)

	logger.Scopef("Worker %d", 3).Warn("busy")
	if !strings.Contains(buffer.String(), "[Worker 3] busy") {
		t.Errorf("got %q, want the formatted tag", buffer.String())
	}
}

func TestCallerTagNamesTheCallingFunction(t *testing.T) {
	buffer := captureOutput(t)

	logger.Info("first")
	logger.Info("second")

	out := buffer.String()
	if count := strings.Count(out, "[TestCallerTagNamesTheCallingFunction]"); count != 2 {
		t.Errorf("got %d tagged lines in %q, want 2 naming the calling function", count, out)
	}
}

func TestScopedDebugRespectsDebugMode(t *testing.T) {
	buffer := captureOutput(t)

	testutil.Swap(t, logger.HookDebugMode, false)
	logger.Scope("x").Debug("hidden")
	logger.Debugf("also hidden")
	if buffer.Len() != 0 {
		t.Errorf("got %q, want nothing while debug mode is off", buffer.String())
	}

	*logger.HookDebugMode = true
	logger.Scope("x").Debug("shown")
	if !strings.Contains(buffer.String(), "shown") {
		t.Error("debug output was suppressed while debug mode is on")
	}
}

func TestSetLogFileWritesToTheFile(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "bot.log")

	logger.SetLogFile(path)
	logger.Info("written to file")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the log file was not created: %v", err)
	}
	if !strings.Contains(string(data), "written to file") {
		t.Errorf("got %q, want the log line in the file", data)
	}
}

func TestSetLogFileFlushesEarlyBuffer(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "bot.log")

	logger.HookOutMu.Lock()
	*logger.HookEarlyBuf = bytes.NewBufferString("buffered before the file existed\n")
	logger.HookOutMu.Unlock()

	logger.SetLogFile(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the log file was not created: %v", err)
	}
	if !strings.Contains(string(data), "buffered before the file existed") {
		t.Errorf("got %q, want the early buffer flushed into the file", data)
	}

	logger.HookOutMu.Lock()
	defer logger.HookOutMu.Unlock()
	if *logger.HookEarlyBuf != nil {
		t.Error("earlyBuf was not cleared after being flushed")
	}
}

func TestSetLogFileDisablesFileOutput(t *testing.T) {
	for _, path := range []string{"", "off", "none"} {
		t.Run("path "+path, func(t *testing.T) {
			resetLogFileState(t)

			real := filepath.Join(t.TempDir(), "bot.log")
			logger.SetLogFile(real)
			logger.SetLogFile(path)

			logger.HookOutMu.Lock()
			defer logger.HookOutMu.Unlock()
			if *logger.HookLogFile != nil {
				t.Error("the log file was left open")
			}
			if *logger.HookOutput != io.Writer(os.Stdout) {
				t.Error("output was not reverted to stdout")
			}
		})
	}
}

func TestSetLogFileIgnoresRepeatedPath(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "bot.log")

	logger.SetLogFile(path)

	logger.HookOutMu.Lock()
	first := *logger.HookLogFile
	logger.HookOutMu.Unlock()

	logger.SetLogFile(path)

	logger.HookOutMu.Lock()
	defer logger.HookOutMu.Unlock()
	if *logger.HookLogFile != first {
		t.Error("setting the same path reopened the file")
	}
}

func TestSetLogFileFallsBackWhenUnopenable(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "missing-dir", "bot.log")

	logger.SetLogFile(path)

	logger.HookOutMu.Lock()
	defer logger.HookOutMu.Unlock()
	if *logger.HookLogFile != nil {
		t.Error("a log file was opened despite the directory not existing")
	}
	if *logger.HookOutput != io.Writer(os.Stdout) {
		t.Error("output did not fall back to stdout")
	}
}
