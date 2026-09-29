package logger

import (
	"fmt"
	"io"
)

const clearLine = "\r\033[K"

var (
	progressLine   string
	progressOffset int64
	progressDecade int
)

func Progress(percent int, message string) {
	line := fmt.Sprintf("%s [%s] %s", infoBadge, callerTag(1), message)

	outMu.Lock()
	defer outMu.Unlock()

	if consoleIsTerminal {
		_, _ = fmt.Fprint(console, clearLine+line)
	} else if decade := percent / 10; decade > progressDecade {
		progressDecade = decade
		_, _ = fmt.Fprintln(console, line)
	}

	if logFile != nil {
		if progressLine == "" {
			progressOffset, _ = logFile.Seek(0, io.SeekEnd)
		}
		writeProgressToFile(line)
	}
	progressLine = line
}

func EndProgress() {
	outMu.Lock()
	defer outMu.Unlock()

	hideProgress()
	progressLine = ""
	progressDecade = 0
}

func writeProgressToFile(line string) {
	text := line + "\n"
	if _, err := logFile.WriteAt([]byte(text), progressOffset); err != nil {
		return
	}
	_ = logFile.Truncate(progressOffset + int64(len(text)))
	_, _ = logFile.Seek(0, io.SeekEnd)
}

func hideProgress() {
	if progressLine == "" {
		return
	}
	if consoleIsTerminal {
		_, _ = fmt.Fprint(console, clearLine)
	}
	if logFile != nil {
		_ = logFile.Truncate(progressOffset)
		_, _ = logFile.Seek(0, io.SeekEnd)
	}
}

func redrawProgress() {
	if progressLine == "" {
		return
	}
	if consoleIsTerminal {
		_, _ = fmt.Fprint(console, clearLine+progressLine)
	}
	if logFile != nil {
		progressOffset, _ = logFile.Seek(0, io.SeekEnd)
		writeProgressToFile(progressLine)
	}
}
