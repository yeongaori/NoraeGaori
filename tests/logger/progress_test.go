package logger_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"noraegaori/internal/logger"
)

func useConsole(t *testing.T, terminal bool) *bytes.Buffer {
	t.Helper()

	buffer := &bytes.Buffer{}

	logger.HookOutMu.Lock()
	previousConsole := *logger.HookConsole
	previousTerminal := *logger.HookConsoleIsTerminal
	previousOutput := *logger.HookOutput
	*logger.HookConsole = buffer
	*logger.HookConsoleIsTerminal = terminal
	*logger.HookOutput = buffer
	*logger.HookProgressLine = ""
	*logger.HookProgressDecade = 0
	logger.HookOutMu.Unlock()

	t.Cleanup(func() {
		logger.HookOutMu.Lock()
		*logger.HookConsole = previousConsole
		*logger.HookConsoleIsTerminal = previousTerminal
		*logger.HookOutput = previousOutput
		*logger.HookProgressLine = ""
		*logger.HookProgressDecade = 0
		logger.HookOutMu.Unlock()
	})

	return buffer
}

func infoLine(tag, message string) string {
	return *logger.HookInfoBadge + " [" + tag + "] " + message
}

func readLog(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read the log file: %v", err)
	}
	return string(data)
}

func TestProgressRewritesTheTerminalLine(t *testing.T) {
	terminal := useConsole(t, true)
	tag := "TestProgressRewritesTheTerminalLine"

	logger.Progress(1, "deno.zip: 1%")
	logger.Progress(2, "deno.zip: 2%")
	logger.EndProgress()

	want := logger.HookClearLine + infoLine(tag, "deno.zip: 1%") + logger.HookClearLine + infoLine(tag, "deno.zip: 2%") + logger.HookClearLine
	if got := terminal.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProgressPrintsEveryTenPercentWhenNotATerminal(t *testing.T) {
	pipe := useConsole(t, false)
	tag := "TestProgressPrintsEveryTenPercentWhenNotATerminal"

	for percent := 1; percent <= 35; percent++ {
		logger.Progress(percent, "ffmpeg.zip")
	}
	logger.EndProgress()
	logger.Progress(5, "deno.zip")
	logger.Progress(10, "deno.zip")

	line := infoLine(tag, "ffmpeg.zip") + "\n"
	want := line + line + line + infoLine(tag, "deno.zip") + "\n"
	if got := pipe.String(); got != want {
		t.Errorf("got %q, want lines at 10, 20 and 30%%, then at 10%% of the next download", got)
	}
}

func TestProgressKeepsOneLineInTheLogFile(t *testing.T) {
	useConsole(t, false)
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "latest.log")
	tag := "TestProgressKeepsOneLineInTheLogFile"
	logger.SetLogFile(path)

	logger.Info("start")
	for percent := 1; percent <= 57; percent++ {
		logger.Progress(percent, "a longer message at the earlier percentages")
	}
	logger.Progress(58, "short")

	want := infoLine(tag, "start") + "\n" + infoLine(tag, "short") + "\n"
	if got := readLog(t, path); got != want {
		t.Errorf("got %q, want the start line and only the latest progress line", got)
	}

	logger.EndProgress()
	logger.Info("done")

	want = infoLine(tag, "start") + "\n" + infoLine(tag, "done") + "\n"
	if got := readLog(t, path); got != want {
		t.Errorf("got %q, want only the start and done lines after the download", got)
	}
}

func TestProgressStaysLastWhenAnotherLineIsLogged(t *testing.T) {
	terminal := useConsole(t, true)
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "latest.log")
	tag := "TestProgressStaysLastWhenAnotherLineIsLogged"
	logger.SetLogFile(path)

	logger.Info("start")
	logger.Progress(40, "deno.zip: 40%")
	logger.Info("other")
	logger.Progress(41, "deno.zip: 41%")

	wantFile := infoLine(tag, "start") + "\n" + infoLine(tag, "other") + "\n" + infoLine(tag, "deno.zip: 41%") + "\n"
	if got := readLog(t, path); got != wantFile {
		t.Errorf("got file %q, want the progress line moved below the new line", got)
	}

	wantTerminal := infoLine(tag, "start") + "\n" +
		logger.HookClearLine + infoLine(tag, "deno.zip: 40%") +
		logger.HookClearLine + infoLine(tag, "other") + "\n" +
		logger.HookClearLine + infoLine(tag, "deno.zip: 40%") +
		logger.HookClearLine + infoLine(tag, "deno.zip: 41%")
	if got := terminal.String(); got != wantTerminal {
		t.Errorf("got terminal %q, want %q", got, wantTerminal)
	}
}

func TestStandardLogOutputKeepsTheProgressLineLast(t *testing.T) {
	useConsole(t, false)
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "latest.log")
	tag := "TestStandardLogOutputKeepsTheProgressLineLast"
	logger.SetLogFile(path)

	logger.Progress(12, "ffmpeg.zip: 12%")
	if _, err := (logger.HookLockedWriter{}).Write([]byte("discordgo says hello\n")); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	want := "discordgo says hello\n" + infoLine(tag, "ffmpeg.zip: 12%") + "\n"
	if got := readLog(t, path); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEndProgressWithoutProgressWritesNothing(t *testing.T) {
	terminal := useConsole(t, true)

	logger.EndProgress()

	if terminal.Len() != 0 {
		t.Errorf("got %q, want nothing written", terminal.String())
	}
}

func TestIsTerminalRejectsARegularFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("failed to create the file: %v", err)
	}
	defer file.Close()

	if logger.HookIsTerminal(file) {
		t.Error("a regular file was treated as a terminal")
	}
}
