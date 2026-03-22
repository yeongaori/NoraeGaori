package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

var (
	debugMode bool

	// Error log file
	errorLogFile *os.File
	errorLogMu   sync.Mutex

	// ANSI color codes matching Node.js colorfulLogger EXACTLY
	// Using 256-color palette codes (\033[48;5;Nm for background)

	// INFO: Lime green background (\033[48;5;10m) with white foreground (\x1b[37m)
	infoBadge = "\033[48;5;10m\x1b[37m INFO \033[0m"

	// WARN: Yellow background (\033[48;5;3m) with white foreground (\x1b[37m)
	warnBadge = "\033[48;5;3m\x1b[37m WARN \033[0m"

	// ERRO: Bright red background (\033[48;5;9m) with white foreground (\x1b[37m)
	errorBadge = "\033[48;5;9m\x1b[37m ERRO \033[0m"

	// DEBG: Orange background (\033[48;5;202m) with white foreground (\x1b[37m)
	debugBadge = "\033[48;5;202m\x1b[37m DEBG \033[0m"

	// TERM: Cyan background (\033[48;5;6m) with white foreground (\x1b[37m)
	termBadge = "\033[48;5;6m\x1b[37m TERM \033[0m"
)

// Initialize sets up the logger with debug mode
func Initialize(debug bool) {
	debugMode = debug
	log.SetOutput(os.Stdout)
	log.SetFlags(0) // We'll handle our own formatting

	// Initialize error log file
	var err error
	errorLogFile, err = os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to open error.log: %v\n", err)
	}
}

// Close closes the error log file
func Close() {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()
	if errorLogFile != nil {
		errorLogFile.Close()
	}
}

// logToFile writes a message to the error log file with timestamp
func logToFile(level, message string) {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()

	if errorLogFile == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)
	errorLogFile.WriteString(line)
}

// Info logs an info message with lime green badge
func Info(message string) {
	fmt.Printf("%s %s\n", infoBadge, message)
}

// Warn logs a warning message with yellow badge
func Warn(message string) {
	fmt.Printf("%s %s\n", warnBadge, message)
	logToFile("WARN", message)
}

// Error logs an error message with red badge
func Error(message string) {
	fmt.Printf("%s %s\n", errorBadge, message)
	logToFile("ERRO", message)
}

// Debug logs a debug message with orange badge (only if debug mode is enabled)
func Debug(message string) {
	if debugMode {
		fmt.Printf("%s %s\n", debugBadge, message)
	}
}

// Term logs a termination message with cyan badge
func Term(message string) {
	fmt.Printf("%s %s\n", termBadge, message)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	Info(fmt.Sprintf(format, args...))
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	Warn(fmt.Sprintf(format, args...))
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	Debug(fmt.Sprintf(format, args...))
}

// IsDebug returns whether debug mode is enabled
func IsDebug() bool {
	return debugMode
}
