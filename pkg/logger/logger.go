package logger

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

var (
	debugMode bool

	errorLogFile *os.File
	errorLogMu   sync.Mutex

	outMu       sync.Mutex
	output      io.Writer = os.Stdout
	earlyBuf    *bytes.Buffer
	logFile     *os.File
	logFilePath string

	infoBadge = "\033[48;5;10m\x1b[37m INFO \033[0m"

	warnBadge = "\033[48;5;3m\x1b[37m WARN \033[0m"

	errorBadge = "\033[48;5;9m\x1b[37m ERRO \033[0m"

	debugBadge = "\033[48;5;202m\x1b[37m DEBG \033[0m"

	termBadge = "\033[48;5;6m\x1b[37m TERM \033[0m"
)

type lockedWriter struct{}

func (lockedWriter) Write(p []byte) (int, error) {
	outMu.Lock()
	defer outMu.Unlock()
	return output.Write(p)
}

func Initialize(debug bool) {
	debugMode = debug

	outMu.Lock()
	earlyBuf = &bytes.Buffer{}
	output = io.MultiWriter(earlyBuf, os.Stdout)
	outMu.Unlock()

	log.SetOutput(lockedWriter{})
	log.SetFlags(0)

	var err error
	errorLogFile, err = os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to open error.log: %v\n", err)
	}
}

func SetLogFile(path string) {
	outMu.Lock()
	defer outMu.Unlock()

	if path == logFilePath {
		return
	}

	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
	logFilePath = path

	if path == "" || path == "off" || path == "none" {
		output = os.Stdout
		earlyBuf = nil
		return
	}

	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		output = os.Stdout
		earlyBuf = nil
		fmt.Printf("Warning: Failed to open log file %s: %v\n", path, err)
		return
	}

	if earlyBuf != nil {
		f.Write(earlyBuf.Bytes())
		earlyBuf = nil
	}
	logFile = f
	output = io.MultiWriter(f, os.Stdout)
}

func Close() {
	outMu.Lock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
	output = os.Stdout
	outMu.Unlock()

	errorLogMu.Lock()
	defer errorLogMu.Unlock()
	if errorLogFile != nil {
		errorLogFile.Close()
	}
}

func printLine(badge, tag, message string) {
	outMu.Lock()
	defer outMu.Unlock()
	fmt.Fprintf(output, "%s [%s] %s\n", badge, tag, message)
}

func logToFile(level, tag, message string) {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()

	if errorLogFile == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] [%s] %s\n", timestamp, level, tag, message)
	errorLogFile.WriteString(line)
}

func emit(badge, fileLevel, tag, message string) {
	printLine(badge, tag, message)
	if fileLevel != "" {
		logToFile(fileLevel, tag, message)
	}
}

func Info(message string) {
	emit(infoBadge, "", callerTag(1), message)
}

func Warn(message string) {
	emit(warnBadge, "WARN", callerTag(1), message)
}

func Error(message string) {
	emit(errorBadge, "ERRO", callerTag(1), message)
}

func Debug(message string) {
	if !debugMode {
		return
	}
	emit(debugBadge, "", callerTag(1), message)
}

func Term(message string) {
	emit(termBadge, "", callerTag(1), message)
}

func Infof(format string, args ...interface{}) {
	emit(infoBadge, "", callerTag(1), fmt.Sprintf(format, args...))
}

func Warnf(format string, args ...interface{}) {
	emit(warnBadge, "WARN", callerTag(1), fmt.Sprintf(format, args...))
}

func Errorf(format string, args ...interface{}) {
	emit(errorBadge, "ERRO", callerTag(1), fmt.Sprintf(format, args...))
}

func Debugf(format string, args ...interface{}) {
	if !debugMode {
		return
	}
	emit(debugBadge, "", callerTag(1), fmt.Sprintf(format, args...))
}

func IsDebug() bool {
	return debugMode
}
