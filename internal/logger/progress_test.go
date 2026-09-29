package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func useConsole(t *testing.T, terminal bool) *bytes.Buffer {
	t.Helper()

	buffer := &bytes.Buffer{}

	outMu.Lock()
	previousConsole := console
	previousTerminal := consoleIsTerminal
	previousOutput := output
	console = buffer
	consoleIsTerminal = terminal
	output = buffer
	progressLine = ""
	progressDecade = 0
	outMu.Unlock()

	t.Cleanup(func() {
		outMu.Lock()
		console = previousConsole
		consoleIsTerminal = previousTerminal
		output = previousOutput
		progressLine = ""
		progressDecade = 0
		outMu.Unlock()
	})

	return buffer
}

func infoLine(tag, message string) string {
	return infoBadge + " [" + tag + "] " + message
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

	Progress(1, "deno.zip: 1%")
	Progress(2, "deno.zip: 2%")
	EndProgress()

	want := clearLine + infoLine(tag, "deno.zip: 1%") + clearLine + infoLine(tag, "deno.zip: 2%") + clearLine
	if got := terminal.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProgressPrintsEveryTenPercentWhenNotATerminal(t *testing.T) {
	pipe := useConsole(t, false)
	tag := "TestProgressPrintsEveryTenPercentWhenNotATerminal"

	for percent := 1; percent <= 35; percent++ {
		Progress(percent, "ffmpeg.zip")
	}
	EndProgress()
	Progress(5, "deno.zip")
	Progress(10, "deno.zip")

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
	SetLogFile(path)

	Info("start")
	for percent := 1; percent <= 57; percent++ {
		Progress(percent, "a longer message at the earlier percentages")
	}
	Progress(58, "short")

	want := infoLine(tag, "start") + "\n" + infoLine(tag, "short") + "\n"
	if got := readLog(t, path); got != want {
		t.Errorf("got %q, want the start line and only the latest progress line", got)
	}

	EndProgress()
	Info("done")

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
	SetLogFile(path)

	Info("start")
	Progress(40, "deno.zip: 40%")
	Info("other")
	Progress(41, "deno.zip: 41%")

	wantFile := infoLine(tag, "start") + "\n" + infoLine(tag, "other") + "\n" + infoLine(tag, "deno.zip: 41%") + "\n"
	if got := readLog(t, path); got != wantFile {
		t.Errorf("got file %q, want the progress line moved below the new line", got)
	}

	wantTerminal := infoLine(tag, "start") + "\n" +
		clearLine + infoLine(tag, "deno.zip: 40%") +
		clearLine + infoLine(tag, "other") + "\n" +
		clearLine + infoLine(tag, "deno.zip: 40%") +
		clearLine + infoLine(tag, "deno.zip: 41%")
	if got := terminal.String(); got != wantTerminal {
		t.Errorf("got terminal %q, want %q", got, wantTerminal)
	}
}

func TestStandardLogOutputKeepsTheProgressLineLast(t *testing.T) {
	useConsole(t, false)
	resetLogFileState(t)
	path := filepath.Join(t.TempDir(), "latest.log")
	tag := "TestStandardLogOutputKeepsTheProgressLineLast"
	SetLogFile(path)

	Progress(12, "ffmpeg.zip: 12%")
	if _, err := (lockedWriter{}).Write([]byte("discordgo says hello\n")); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	want := "discordgo says hello\n" + infoLine(tag, "ffmpeg.zip: 12%") + "\n"
	if got := readLog(t, path); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEndProgressWithoutProgressWritesNothing(t *testing.T) {
	terminal := useConsole(t, true)

	EndProgress()

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

	if isTerminal(file) {
		t.Error("a regular file was treated as a terminal")
	}
}
