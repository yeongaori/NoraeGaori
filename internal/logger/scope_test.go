package logger

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noraegaori/internal/testutil"
)

func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()

	buffer := &bytes.Buffer{}

	outMu.Lock()
	previous := output
	output = buffer
	outMu.Unlock()

	t.Cleanup(func() {
		outMu.Lock()
		output = previous
		outMu.Unlock()
	})

	return buffer
}

func resetLogFileState(t *testing.T) {
	t.Helper()

	outMu.Lock()
	previousOutput := output
	previousFile := logFile
	previousPath := logFilePath
	previousBuf := earlyBuf
	logFile = nil
	logFilePath = ""
	earlyBuf = nil
	outMu.Unlock()

	t.Cleanup(func() {
		outMu.Lock()
		if logFile != nil && logFile != previousFile {
			logFile.Close()
		}
		output = previousOutput
		logFile = previousFile
		logFilePath = previousPath
		earlyBuf = previousBuf
		outMu.Unlock()
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
		if got := deriveTag(fullName); got != want {
			t.Errorf("deriveTag(%q) = %q, want %q", fullName, got, want)
		}
	}
}

func TestDeriveTagFallsBackToPackageName(t *testing.T) {
	if got := deriveTag("noraegaori/internal/player.func1"); got != "player" {
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
		if got := isDigits(input); got != want {
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
		if got := isGeneratedPart(input); got != want {
			t.Errorf("isGeneratedPart(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestScopeUsesTheGivenTag(t *testing.T) {
	buffer := captureOutput(t)

	scope := Scope("Worker 7")
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

	Scopef("Worker %d", 3).Warn("busy")
	if !strings.Contains(buffer.String(), "[Worker 3] busy") {
		t.Errorf("got %q, want the formatted tag", buffer.String())
	}
}

func TestCallerTagNamesTheCallingFunction(t *testing.T) {
	buffer := captureOutput(t)

	Info("first")
	Info("second")

	out := buffer.String()
	if count := strings.Count(out, "[TestCallerTagNamesTheCallingFunction]"); count != 2 {
		t.Errorf("got %d tagged lines in %q, want 2 naming the calling function", count, out)
	}
}

func TestScopedDebugRespectsDebugMode(t *testing.T) {
	buffer := captureOutput(t)

	testutil.Swap(t, &debugMode, false)
	Scope("x").Debug("hidden")
	Debugf("also hidden")
	if buffer.Len() != 0 {
		t.Errorf("got %q, want nothing while debug mode is off", buffer.String())
	}

	debugMode = true
	Scope("x").Debug("shown")
	if !strings.Contains(buffer.String(), "shown") {
		t.Error("debug output was suppressed while debug mode is on")
	}
}

func TestSetLogFileWritesToTheFile(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "bot.log")

	SetLogFile(path)
	Info("written to file")

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

	outMu.Lock()
	earlyBuf = bytes.NewBufferString("buffered before the file existed\n")
	outMu.Unlock()

	SetLogFile(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the log file was not created: %v", err)
	}
	if !strings.Contains(string(data), "buffered before the file existed") {
		t.Errorf("got %q, want the early buffer flushed into the file", data)
	}

	outMu.Lock()
	defer outMu.Unlock()
	if earlyBuf != nil {
		t.Error("earlyBuf was not cleared after being flushed")
	}
}

func TestSetLogFileDisablesFileOutput(t *testing.T) {
	for _, path := range []string{"", "off", "none"} {
		t.Run("path "+path, func(t *testing.T) {
			resetLogFileState(t)

			real := filepath.Join(t.TempDir(), "bot.log")
			SetLogFile(real)
			SetLogFile(path)

			outMu.Lock()
			defer outMu.Unlock()
			if logFile != nil {
				t.Error("the log file was left open")
			}
			if output != io.Writer(os.Stdout) {
				t.Error("output was not reverted to stdout")
			}
		})
	}
}

func TestSetLogFileIgnoresRepeatedPath(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "bot.log")

	SetLogFile(path)

	outMu.Lock()
	first := logFile
	outMu.Unlock()

	SetLogFile(path)

	outMu.Lock()
	defer outMu.Unlock()
	if logFile != first {
		t.Error("setting the same path reopened the file")
	}
}

func TestSetLogFileFallsBackWhenUnopenable(t *testing.T) {
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "missing-dir", "bot.log")

	SetLogFile(path)

	outMu.Lock()
	defer outMu.Unlock()
	if logFile != nil {
		t.Error("a log file was opened despite the directory not existing")
	}
	if output != io.Writer(os.Stdout) {
		t.Error("output did not fall back to stdout")
	}
}
